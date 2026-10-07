"""A recognizer that runs a token classification model under ONNX Runtime, in bounded chunks.

The model directory holds what `export_model.py` wrote at build time: the quantized model,
its tokenizer and its labels. Nothing here needs PyTorch nor transformers.

What it guarantees to a caller:

- Only the labels of `label_mapping` are returned, under the entity type they map to: the
  image declares a fixed list of entity types.
- Presidio's chunker ends a chunk on a space or a new line only, however far that is, while
  the model reads a fixed number of tokens. Here a chunk never exceeds `chunk_size`
  characters, and a chunk whose tokens still exceed the model's window is split again
  before it is handed to the model: nothing is truncated.
- Presidio keeps, of two overlapping findings of two chunks, the one of the higher score.
  Here they become one finding that covers both.
- An inference that fails is an error of the request, not an empty answer.
"""

import json
import os
from pathlib import Path
from typing import Dict, List, Optional, Sequence, Tuple

import numpy as np
import onnxruntime
from presidio_analyzer import AnalysisExplanation, LocalRecognizer, RecognizerResult
from presidio_analyzer.chunkers import (
    BaseTextChunker,
    CharacterBasedTextChunker,
    TextChunk,
)
from presidio_analyzer.input_validation import yaml_recognizer_models
from tokenizers import Tokenizer

# The number of threads of one inference. ONNX Runtime sizes its pool on the cores of the
# host, not on the CPU quota of the container: the image sets this variable.
THREADS_VARIABLE = "OMP_NUM_THREADS"


class BoundedTextChunker(CharacterBasedTextChunker):
    """Chunks of at most `chunk_size` characters, cut on word boundaries when there are some."""

    @property
    def start_reach(self) -> int:
        """How far before its fixed start a chunk may begin, to begin on a word.

        One more overlap: two chunks then share between one and two overlaps, and a word
        longer than that is not searched for. Capped so that a chunk of `chunk_size`
        characters still ends past the start of the next one.
        """
        return min(self.chunk_overlap, self.chunk_size - self.chunk_overlap - 1)

    def chunk(self, text: str) -> List[TextChunk]:
        """Split text into overlapping chunks, none longer than `chunk_size`.

        A chunk ends on the last boundary character found within `chunk_size`; without one
        it ends at `chunk_size` characters. The next chunk starts `chunk_overlap` characters
        before that end, or on the nearest word start before it within `start_reach`: two
        consecutive chunks always share at least `chunk_overlap` characters, so a span of
        that length or less is whole in one of them.
        """
        chunks = []
        start = 0
        while start < len(text):
            if len(chunks) >= self.most_chunks(len(text)):
                raise RuntimeError("more chunks than a text of this length can need")
            end = min(start + self.chunk_size, len(text))
            if end < len(text):
                # A cut any closer to the start would not let the next chunk advance.
                closest = start + self.chunk_overlap + self.start_reach + 1
                cut = self._last_boundary(text, closest, end + 1)
                if cut != -1:
                    end = cut
            chunks.append(TextChunk(text=text[start:end], start=start, end=end))
            if end >= len(text):
                break
            following = end - self.chunk_overlap
            before_word = self._last_boundary(
                text, following - self.start_reach - 1, following
            )
            if before_word != -1:
                following = before_word + 1
            if following <= start:
                raise RuntimeError("the chunks of the text do not advance")
            start = following
        return chunks

    def most_chunks(self, length: int) -> int:
        """The most chunks a text of this length is split into.

        Two chunks in a row move the end forward by at least `chunk_size` less the most two
        chunks share. The loop of `chunk` stops with an error past this count, whatever
        keeps it from advancing.
        """
        step = self.chunk_size - self.chunk_overlap - self.start_reach
        return 2 * (length // step + 1) + 1

    def _last_boundary(self, text: str, low: int, high: int) -> int:
        return max(text.rfind(char, low, high) for char in self.boundary_chars)

    def predict_with_chunking(self, text, predict_func) -> List[RecognizerResult]:
        """Predict each chunk and merge the findings, at their positions in the text."""
        chunks = self.chunk(text)
        if len(chunks) <= 1:
            return predict_func(text) if chunks else []

        found = []
        for index, chunk in enumerate(chunks):
            for result in predict_func(chunk.text):
                found.append(
                    (index, _span(result, result.start + chunk.start, result.end + chunk.start))
                )
        return self.merge_across_chunks(found)

    @staticmethod
    def merge_across_chunks(
        found: List[Tuple[int, RecognizerResult]],
    ) -> List[RecognizerResult]:
        """Replace the findings of different chunks that overlap by their union.

        Two chunks share a part of the text and each reads a name there with its own
        context: one may return it whole and the other a part of it. No winner is chosen.
        Findings of one entity type that overlap, directly or through others, and do not
        all come from one chunk become one finding: from the smallest start to the largest
        end, with the score and the explanation of the highest score (the first given, of
        equal scores). Findings that overlap and all come from one chunk are returned as
        they are. No character a chunk designated is dropped, and none is added.

        The findings are sorted once and read in one pass.

        :param found: pairs of a chunk's index and one of its findings, at its position in
            the text.
        :return: the findings by start; of equal starts, in the order they were given.
        """
        by_type: Dict[str, List[Tuple[int, int, int, int, RecognizerResult]]] = {}
        for position, (chunk, result) in enumerate(found):
            by_type.setdefault(result.entity_type, []).append(
                (result.start, result.end, position, chunk, result)
            )

        merged: List[Tuple[int, int, RecognizerResult]] = []
        for of_type in by_type.values():
            of_type.sort(key=lambda item: item[:3])
            group = [of_type[0]]
            group_end = of_type[0][1]
            for item in of_type[1:]:
                if item[0] >= group_end:
                    merged.extend(_united(group))
                    group = []
                    group_end = item[1]
                group.append(item)
                group_end = max(group_end, item[1])
            merged.extend(_united(group))
        merged.sort(key=lambda item: item[:2])
        return [result for _, _, result in merged]


def _united(
    group: List[Tuple[int, int, int, int, RecognizerResult]],
) -> List[Tuple[int, int, RecognizerResult]]:
    """Unite findings that overlap, unless one chunk returned them all.

    :param group: start, end, position given, chunk and finding of each.
    :return: start, position and finding of what the group becomes.
    """
    if len({chunk for _, _, _, chunk, _ in group}) == 1:
        return [(start, position, result) for start, _, position, _, result in group]
    best = max(group, key=lambda item: (item[4].score, -item[2]))[4]
    start = min(item[0] for item in group)
    end = max(item[1] for item in group)
    position = min(item[2] for item in group)
    return [(start, position, _span(best, start, end))]


def _span(result: RecognizerResult, start: int, end: int) -> RecognizerResult:
    """Copy a finding to other positions."""
    return RecognizerResult(
        entity_type=result.entity_type,
        start=start,
        end=end,
        score=result.score,
        analysis_explanation=result.analysis_explanation,
        recognition_metadata=result.recognition_metadata,
    )


def grouped(
    labels: Sequence[str],
    scores: Sequence[float],
    offsets: Sequence[Tuple[int, int]],
    special: Sequence[int],
) -> List[Tuple[str, int, int, float]]:
    """Group the tokens of a text into entities, as the "simple" strategy of transformers.

    Consecutive tokens of one tag are one entity: from the start of the first token to the
    end of the last, with the mean of their scores. A `B-` label starts a new entity. The
    tokens labelled `O` separate entities and are not returned; special tokens are skipped.

    :param labels: the label of each token (`O`, `PER`, `I-PER`, `B-PER`...).
    :param scores: the probability of that label for each token.
    :param offsets: the start and end of each token, in characters of the text.
    :param special: for each token, whether it is a special token of the tokenizer.
    :return: tag, start, end and score of each entity, in the order of the text.
    """
    groups: List[Tuple[str, int, int, List[float]]] = []
    for label, score, (start, end), is_special in zip(labels, scores, offsets, special):
        if is_special:
            continue
        begins = label.startswith("B-")
        tag = label[2:] if label[:2] in ("B-", "I-") else label
        if groups and groups[-1][0] == tag and not begins:
            _, first_start, _, token_scores = groups[-1]
            token_scores.append(float(score))
            groups[-1] = (tag, first_start, end, token_scores)
        else:
            groups.append((tag, start, end, [float(score)]))
    return [
        (tag, start, end, sum(token_scores) / len(token_scores))
        for tag, start, end, token_scores in groups
        if tag != "O"
    ]


class OnnxNerRecognizer(LocalRecognizer):
    """Find the entities of `label_mapping` with a model run by ONNX Runtime.

    The registry file is validated with the fields of Presidio's Hugging Face recognizer,
    the only ones its registry hands over. This recognizer takes five of them and refuses
    the others when they are set.

    :param model_name: directory of `model.onnx`, `tokenizer.json` and `ner.json`.
    :param label_mapping: the model's tags that are returned, and the entity type of each.
    :param threshold: the score below which an entity is not returned.
    :param chunk_size: the most characters handed to the model at once.
    :param chunk_overlap: the least characters two consecutive chunks share.
    :param other_settings: the fields of the registry file this recognizer does not take.
    """

    CHUNK_SIZE = 400
    CHUNK_OVERLAP = 40

    def __init__(
        self,
        model_name: Optional[str] = None,
        label_mapping: Optional[Dict[str, str]] = None,
        threshold: Optional[float] = None,
        chunk_size: Optional[int] = None,
        chunk_overlap: Optional[int] = None,
        supported_entities: Optional[List[str]] = None,
        supported_language: str = "fr",
        name: Optional[str] = None,
        context: Optional[List[str]] = None,
        **other_settings,
    ):
        missing = [
            setting
            for setting, value in (
                ("model_name", model_name),
                ("label_mapping", label_mapping),
                ("threshold", threshold),
            )
            if value is None
        ]
        if missing:
            raise ValueError(f"{type(self).__name__} needs {', '.join(missing)}")
        unknown = sorted(setting for setting, value in other_settings.items() if value is not None)
        if unknown:
            raise ValueError(f"{type(self).__name__} has no setting {', '.join(unknown)}")

        self.model_path = Path(model_name)
        self.label_mapping = dict(label_mapping)
        self.threshold = threshold
        self.text_chunker = BoundedTextChunker(
            chunk_size=self.CHUNK_SIZE if chunk_size is None else chunk_size,
            chunk_overlap=self.CHUNK_OVERLAP if chunk_overlap is None else chunk_overlap,
        )
        mapped = sorted(set(self.label_mapping.values()))
        if supported_entities is not None and sorted(supported_entities) != mapped:
            raise ValueError(
                f"supported_entities {supported_entities} are not the entity types of "
                f"label_mapping {mapped}"
            )
        super().__init__(
            supported_entities=mapped,
            supported_language=supported_language,
            name=name,
            context=context,
        )

    def load(self) -> None:
        """Load the tokenizer, the labels and the model."""
        described = json.loads((self.model_path / "ner.json").read_text(encoding="utf-8"))
        self.model_name: str = described["source"]
        self.labels: List[str] = described["labels"]
        self.max_tokens: int = described["max_tokens"]

        self.tokenizer = Tokenizer.from_file(str(self.model_path / "tokenizer.json"))
        # The chunks are bounded here: a text the tokenizer would cut is split instead.
        self.tokenizer.no_truncation()
        self.tokenizer.no_padding()

        options = onnxruntime.SessionOptions()
        options.intra_op_num_threads = int(os.environ.get(THREADS_VARIABLE, "0"))
        options.inter_op_num_threads = 1
        self.session = onnxruntime.InferenceSession(
            str(self.model_path / "model.onnx"), options, providers=["CPUExecutionProvider"]
        )

    def analyze(
        self, text: str, entities: List[str], nlp_artifacts=None
    ) -> List[RecognizerResult]:
        """Return the entities of the text among those asked for."""
        if not text or not set(entities) & set(self.supported_entities):
            return []
        results = self.text_chunker.predict_with_chunking(text, self._predict_chunk)
        return [result for result in results if result.entity_type in entities]

    def _predict_chunk(self, chunk_text: str) -> List[RecognizerResult]:
        encoding = self.tokenizer.encode(chunk_text)
        if len(encoding.ids) > self.max_tokens:
            return self._predict_in_halves(chunk_text)

        logits = self.session.run(
            ["logits"],
            {
                "input_ids": np.array([encoding.ids], dtype=np.int64),
                "attention_mask": np.ones((1, len(encoding.ids)), dtype=np.int64),
            },
        )[0][0]
        exponentials = np.exp(logits - logits.max(axis=-1, keepdims=True))
        probabilities = exponentials / exponentials.sum(axis=-1, keepdims=True)
        best = probabilities.argmax(axis=-1)
        entities = grouped(
            [self.labels[index] for index in best],
            probabilities[np.arange(len(best)), best],
            encoding.offsets,
            encoding.special_tokens_mask,
        )

        results = []
        for tag, start, end, score in entities:
            entity_type = self.label_mapping.get(tag)
            if entity_type is None or score < self.threshold:
                continue
            explanation = AnalysisExplanation(
                recognizer=self.name,
                original_score=score,
                textual_explanation=(
                    f"Identified as {entity_type} by {self.model_name} (label: {tag})"
                ),
            )
            results.append(
                RecognizerResult(
                    entity_type=entity_type,
                    start=start,
                    end=end,
                    score=score,
                    analysis_explanation=explanation,
                )
            )
        return results

    def _predict_in_halves(self, chunk_text: str) -> List[RecognizerResult]:
        """Predict a chunk whose tokens exceed the model's window as smaller chunks.

        A character can be several tokens, so a bound in characters does not bound the tokens.
        """
        size = len(chunk_text) // 2
        if size == 0:
            raise ValueError("one character exceeds the model's window")
        halves = BoundedTextChunker(
            chunk_size=size,
            chunk_overlap=min(self.text_chunker.chunk_overlap, size // 2),
        )
        return halves.predict_with_chunking(chunk_text, self._predict_chunk)


def _require(present: bool, what: str) -> None:
    if not present:
        raise ImportError(
            f"{__name__} relies on {what}, which this version of Presidio does not have"
        )


# What this module overrides or calls in Presidio is not a public interface. A name that is
# gone would leave an override unused without an error: the module refuses to load instead.
_require(
    callable(getattr(BaseTextChunker, "predict_with_chunking", None)),
    "BaseTextChunker.predict_with_chunking",
)
_require(
    all(
        isinstance(getattr(CharacterBasedTextChunker, name, None), property)
        for name in ("chunk_size", "chunk_overlap", "boundary_chars")
    ),
    "the chunk_size, chunk_overlap and boundary_chars properties of CharacterBasedTextChunker",
)
_hugging_face_config = getattr(yaml_recognizer_models, "HuggingFaceRecognizerConfig", None)
_require(
    _hugging_face_config is not None
    and isinstance(getattr(yaml_recognizer_models, "CONFIG_MODEL_MAP", None), dict)
    and {"model_name", "label_mapping", "threshold", "chunk_size", "chunk_overlap"}
    <= set(getattr(_hugging_face_config, "model_fields", {})),
    "yaml_recognizer_models.CONFIG_MODEL_MAP and the fields of HuggingFaceRecognizerConfig",
)

# The registry file is validated per class name, and the registry only hands over the fields
# of the configuration models Presidio declares itself. Without this line a class of this
# image is validated as a plain predefined recognizer, and model_name, label_mapping and
# threshold are dropped before the recognizer is built.
yaml_recognizer_models.CONFIG_MODEL_MAP[OnnxNerRecognizer.__name__] = _hugging_face_config
