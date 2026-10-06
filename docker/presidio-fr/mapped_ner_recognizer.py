"""A Hugging Face NER recognizer that returns only mapped labels, in bounded chunks.

Three things differ from Presidio's HuggingFaceNerRecognizer (2.2.362):

- A label absent from `label_mapping` is returned by Presidio under the model's own name.
  Here it is dropped: the image declares a fixed list of entity types.
- Presidio's chunker ends a chunk on a space or a new line only, however far that is, while
  the model reads a fixed number of tokens and the pipeline truncates what exceeds it. Here
  a chunk never exceeds `chunk_size` characters, and a chunk whose tokens still exceed the
  model's window is split again before it is handed to the model.
- Presidio returns no finding for a chunk the pipeline raises on. Here the error is raised.
"""

import inspect
from typing import List

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
            start = end - self.chunk_overlap
            before_word = self._last_boundary(text, start - self.start_reach - 1, start)
            if before_word != -1:
                start = before_word + 1
        return chunks

    def _last_boundary(self, text: str, low: int, high: int) -> int:
        return max(text.rfind(char, low, high) for char in self.boundary_chars)


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
