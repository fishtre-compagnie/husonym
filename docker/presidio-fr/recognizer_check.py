"""Checks of mapped_ner_recognizer.py that need neither PyTorch nor the model.

The image build runs this file: a failed check fails the build. It also runs alone in the
base image, see the README.
"""

import random
from typing import Dict, List

from presidio_analyzer.chunkers import TextChunk

from mapped_ner_recognizer import BoundedTextChunker, MappedLabelsNerRecognizer

CHUNK_SIZE = 400
CHUNK_OVERLAP = 40
NAMES = ["Corentin Le Guével", "Mathilde De La Roche"]


def check_chunks(text: str, size: int = CHUNK_SIZE, overlap: int = CHUNK_OVERLAP) -> List[TextChunk]:
    """Chunk text and check what holds for any text."""
    chunks = BoundedTextChunker(chunk_size=size, chunk_overlap=overlap).chunk(text)
    if not text:
        assert chunks == [], chunks
        return chunks
    assert chunks[0].start == 0, chunks[0]
    assert chunks[-1].end == len(text), chunks[-1]
    for chunk in chunks:
        assert 0 < chunk.end - chunk.start <= size, chunk
        assert chunk.text == text[chunk.start : chunk.end], chunk
    for previous, following in zip(chunks, chunks[1:]):
        assert following.start <= previous.end - overlap, (previous, following)
        assert following.start > previous.start, (previous, following)
        assert following.end > previous.end, (previous, following)
    return chunks


def is_whole_in_a_chunk(chunks: List[TextChunk], start: int, end: int) -> bool:
    return any(chunk.start <= start and end <= chunk.end for chunk in chunks)


def check_random_texts() -> int:
    """Texts of several alphabets, with much, little and no whitespace, and several sizes."""
    rng = random.Random(20261006)
    alphabets = ["abcdefghij", "aé😀́œ;,", "0123456789-_/"]
    sizes = [(400, 40), (400, 0), (100, 40), (50, 49), (7, 3), (2, 1), (1, 0)]
    count = 0
    for alphabet in alphabets:
        for whitespace_share in (0.0, 0.002, 0.02, 0.15, 0.6):
            for size, overlap in sizes:
                for _ in range(12):
                    length = rng.choice([1, 2, size - 1, size, size + 1, 3 * size, 1500])
                    text = "".join(
                        rng.choice(" \n") if rng.random() < whitespace_share else rng.choice(alphabet)
                        for _ in range(max(length, 1))
                    )
                    chunks = check_chunks(text, size, overlap)
                    for _ in range(20):
                        span = rng.randint(0, overlap)
                        start = rng.randint(0, max(len(text) - span, 0))
                        assert is_whole_in_a_chunk(chunks, start, min(start + span, len(text)))
                    count += 1
    return count


def check_names_in_text_with_little_whitespace() -> int:
    """A name of several words is whole in a chunk wherever it stands around the cuts."""
    count = 0
    for name in NAMES:
        for filler in ("x", "ref-0001;", '{"k":"v"},'):
            for offset in range(280, 480):
                text = (filler * offset)[:offset] + name + " est venu," + (filler * 500)[:500]
                chunks = check_chunks(text)
                assert is_whole_in_a_chunk(chunks, offset, offset + len(name)), (name, offset)
                count += 1
    return count


def check_named_texts() -> None:
    assert check_chunks("") == []
    assert [(c.start, c.end) for c in check_chunks("Bonjour, je suis là.")] == [(0, 20)]
    unbroken = check_chunks("x" * 5000)
    assert [(c.start, c.end) for c in unbroken[:3]] == [(0, 400), (360, 760), (720, 1120)]
    assert len(unbroken) == 14, len(unbroken)
    # A cut on a boundary, then a start on the word that precedes the fixed start.
    sentences = "Le suivi indique un passage au dépôt. " * 30
    for chunk in check_chunks(sentences):
        assert chunk.start == 0 or sentences[chunk.start - 1] == " ", chunk
        assert chunk.end == len(sentences) or sentences[chunk.end] == " ", chunk
    # Characters of several bytes, combining accents and characters outside the basic plane
    # at the cuts: positions count characters, and each chunk is the exact slice.
    check_chunks("é" * 600)
    check_chunks("😀" * 900)
    check_chunks("é" * 399 + " " + "œ" * 300)
    check_chunks("é " * 400)


class FakeTokenizer:
    """One token per character, two for a fraction sign, plus the two special tokens."""

    model_max_length = 512

    def __call__(self, text: str, truncation: bool) -> Dict[str, List[int]]:
        assert truncation is False
        return {"input_ids": [0] * (len(text) + text.count("½") + 2)}


class FakePipeline:
    """Finds the names as PER, « Besançon » as LOC and « Faible » as a PER of low score."""

    tokenizer = FakeTokenizer()

    def __init__(self) -> None:
        self.largest_input = 0

    def __call__(self, text: str) -> List[dict]:
        tokens = len(self.tokenizer(text, truncation=False)["input_ids"])
        assert tokens <= self.tokenizer.model_max_length, tokens
        self.largest_input = max(self.largest_input, tokens)
        if "panne" in text:
            raise RuntimeError("inference failed")
        found = []
        for word, label, score in [(n, "PER", 0.99) for n in NAMES] + [
            ("Besançon", "LOC", 0.99),
            ("Faible", "PER", 0.5),
        ]:
            position = text.find(word)
            while position != -1:
                found.append(
                    {"entity_group": label, "score": score, "start": position, "end": position + len(word)}
                )
                position = text.find(word, position + 1)
        return found


def fake_recognizer() -> MappedLabelsNerRecognizer:
    """The recognizer without its constructor, which needs transformers and PyTorch."""
    recognizer = MappedLabelsNerRecognizer.__new__(MappedLabelsNerRecognizer)
    recognizer.name = "MappedLabelsNerRecognizer"
    recognizer.model_name = "fake"
    recognizer.supported_entities = ["PERSON"]
    recognizer.label_mapping = {"PER": "PERSON"}
    recognizer.label_prefixes = ["B-", "I-"]
    recognizer.threshold = 0.8
    recognizer.text_chunker = BoundedTextChunker(chunk_size=CHUNK_SIZE, chunk_overlap=CHUNK_OVERLAP)
    recognizer.ner_pipeline = FakePipeline()
    return recognizer


def found(recognizer: MappedLabelsNerRecognizer, text: str) -> List[tuple]:
    return [(r.entity_type, text[r.start : r.end]) for r in recognizer.analyze(text, ["PERSON"])]


def check_predictions() -> None:
    recognizer = fake_recognizer()
    name = NAMES[0]

    # Only mapped labels at or above the threshold, under the recognizer's name.
    text = f"Faible. {name} travaille à Besançon."
    results = recognizer.analyze(text, ["PERSON"])
    assert [(r.entity_type, text[r.start : r.end], r.score) for r in results] == [("PERSON", name, 0.99)]
    assert results[0].analysis_explanation.recognizer == recognizer.name

    # A name around a cut is returned once, whole, at its position in the text.
    for offset in range(280, 480, 7):
        text = ("ref-0001;" * 60)[:offset] + name + ";" + "ref-0002;" * 60
        assert found(recognizer, text) == [("PERSON", name)], offset

    # A chunk of more tokens than the model's window is split before the pipeline reads it.
    text = "½" * 800 + " " + name + "."
    assert found(recognizer, text) == [("PERSON", name)]
    assert recognizer.ner_pipeline.largest_input <= FakeTokenizer.model_max_length

    # An inference that fails is an error, in the first chunk as in a later one.
    for text in ("En panne.", "x" * 900 + " en panne."):
        try:
            recognizer.analyze(text, ["PERSON"])
        except RuntimeError:
            continue
        raise AssertionError("a failed inference returned findings")


if __name__ == "__main__":
    random_texts = check_random_texts()
    names = check_names_in_text_with_little_whitespace()
    check_named_texts()
    check_predictions()
    print(f"recognizer checks passed: {random_texts} random texts, {names} name positions")
