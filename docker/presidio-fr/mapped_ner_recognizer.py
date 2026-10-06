"""A Hugging Face NER recognizer that returns only mapped labels and reads the whole text.

Two things differ from Presidio's HuggingFaceNerRecognizer (2.2.362):

- A label absent from `label_mapping` is returned by Presidio under the model's own name.
  Here it is dropped: the image declares a fixed list of entity types.
- Presidio's chunker ends a chunk on a space or a new line only, however far that is, while
  the model reads a fixed number of tokens and the pipeline truncates what exceeds it. Here
  a chunk never exceeds `chunk_size` characters, and a chunk whose tokens still exceed the
  model's window is split again: every character of the text is read by the model.
"""

from typing import List

from presidio_analyzer import RecognizerResult
from presidio_analyzer.chunkers import CharacterBasedTextChunker, TextChunk
from presidio_analyzer.input_validation import yaml_recognizer_models
from presidio_analyzer.predefined_recognizers import HuggingFaceNerRecognizer


class BoundedTextChunker(CharacterBasedTextChunker):
    """Chunks of at most `chunk_size` characters, cut on word boundaries when there are some."""

    def chunk(self, text: str) -> List[TextChunk]:
        """Split text into overlapping chunks, none longer than `chunk_size`.

        A chunk ends on the last boundary character found within `chunk_size`, past its
        overlap with the previous chunk; without one it ends at `chunk_size` characters.
        The next chunk starts on the first word of the overlap; without a boundary in the
        overlap it starts `chunk_overlap` characters before the end.
        """
        chunks = []
        start = 0
        while start < len(text):
            end = min(start + self.chunk_size, len(text))
            if end < len(text):
                # A cut within the overlap would not let the next chunk advance.
                cut = self._last_boundary(text, start + self.chunk_overlap + 1, end + 1)
                if cut != -1:
                    end = cut
            chunks.append(TextChunk(text=text[start:end], start=start, end=end))
            if end >= len(text):
                break
            start = end - self.chunk_overlap
            before_word = self._first_boundary(text, start - 1, end)
            if before_word != -1:
                start = before_word + 1
        return chunks

    def _last_boundary(self, text: str, low: int, high: int) -> int:
        return max(text.rfind(char, low, high) for char in self.boundary_chars)

    def _first_boundary(self, text: str, low: int, high: int) -> int:
        found = [
            index
            for char in self.boundary_chars
            if (index := text.find(char, low, high)) != -1
        ]
        return min(found, default=-1)


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
        kept = set(self.label_mapping.values())
        return [r for r in super()._predict_chunk(chunk_text) if r.entity_type in kept]

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


# The registry file is validated per class name: without this line the fields of the
# Hugging Face recognizer (model_name, label_mapping, threshold...) are dropped for a
# subclass, which is validated as a plain predefined recognizer.
yaml_recognizer_models.CONFIG_MODEL_MAP[MappedLabelsNerRecognizer.__name__] = (
    yaml_recognizer_models.HuggingFaceRecognizerConfig
)
