"""A Hugging Face NER recognizer that returns only mapped labels, in bounded chunks.

Four things differ from Presidio's HuggingFaceNerRecognizer (2.2.362):

- A label absent from `label_mapping` is returned by Presidio under the model's own name.
  Here it is dropped: the image declares a fixed list of entity types.
- Presidio's chunker ends a chunk on a space or a new line only, however far that is, while
  the model reads a fixed number of tokens and the pipeline truncates what exceeds it. Here
  a chunk never exceeds `chunk_size` characters, and a chunk whose tokens still exceed the
  model's window is split again before it is handed to the model.
- Presidio keeps, of two overlapping findings of two chunks, the one of the higher score.
  Here they become one finding that covers both.
- Presidio returns no finding for a chunk the pipeline raises on. Here the error is raised.
"""

import inspect
from typing import Dict, List, Tuple

from presidio_analyzer import AnalysisExplanation, RecognizerResult
from presidio_analyzer.chunkers import (
    BaseTextChunker,
    CharacterBasedTextChunker,
    TextChunk,
)
from presidio_analyzer.input_validation import yaml_recognizer_models
from presidio_analyzer.predefined_recognizers import HuggingFaceNerRecognizer


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


class MappedLabelsNerRecognizer(HuggingFaceNerRecognizer):
    """Keep the findings whose label is in `label_mapping`; read the text in bounded chunks."""

    def __init__(self, chunk_size: int = 400, chunk_overlap: int = 40, **kwargs):
        super().__init__(
            text_chunker=BoundedTextChunker(
                chunk_size=chunk_size, chunk_overlap=chunk_overlap
            ),
            **kwargs,
        )

    def _predict_chunk(self, chunk_text: str) -> List[RecognizerResult]:
        tokenizer = self.ner_pipeline.tokenizer
        tokens = tokenizer(chunk_text, truncation=False)["input_ids"]
        if len(tokens) > tokenizer.model_max_length:
            return self._predict_in_halves(chunk_text)

        results = []
        for prediction in self.ner_pipeline(chunk_text):
            label = prediction.get("entity_group") or prediction["entity"]
            entity_type = self.label_mapping.get(self._normalize_label(label))
            score = float(prediction["score"])
            if entity_type is None or score < self.threshold:
                continue
            explanation = AnalysisExplanation(
                recognizer=self.name,
                original_score=score,
                textual_explanation=(
                    f"Identified as {entity_type} by {self.model_name} (label: {label})"
                ),
            )
            results.append(
                RecognizerResult(
                    entity_type=entity_type,
                    start=prediction["start"],
                    end=prediction["end"],
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
    callable(getattr(HuggingFaceNerRecognizer, "_predict_chunk", None)),
    "HuggingFaceNerRecognizer._predict_chunk",
)
_require(
    callable(getattr(HuggingFaceNerRecognizer, "_normalize_label", None)),
    "HuggingFaceNerRecognizer._normalize_label",
)
_require(
    "text_chunker" in inspect.signature(HuggingFaceNerRecognizer.__init__).parameters,
    "the text_chunker parameter of HuggingFaceNerRecognizer",
)
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
    and getattr(yaml_recognizer_models, "CONFIG_MODEL_MAP", {}).get(
        HuggingFaceNerRecognizer.__name__
    )
    is _hugging_face_config,
    "yaml_recognizer_models.CONFIG_MODEL_MAP with HuggingFaceRecognizerConfig",
)

# The registry file is validated per class name: without this line the fields of the
# Hugging Face recognizer (model_name, label_mapping, threshold...) are dropped for a
# subclass, which is validated as a plain predefined recognizer.
yaml_recognizer_models.CONFIG_MODEL_MAP[MappedLabelsNerRecognizer.__name__] = _hugging_face_config
