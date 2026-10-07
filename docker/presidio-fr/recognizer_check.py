"""Checks of mapped_ner_recognizer.py that need neither PyTorch nor the model.

The image build runs this file: a failed check fails the build. It also runs alone in the
base image, see the README.
"""

import random
from typing import Dict, List, Set, Tuple

from presidio_analyzer import AnalysisExplanation, RecognizerResult
from presidio_analyzer.chunkers import TextChunk

from mapped_ner_recognizer import BoundedTextChunker, MappedLabelsNerRecognizer

CHUNK_SIZE = 400
CHUNK_OVERLAP = 40
NAMES = ["Corentin Le Guével", "Mathilde De La Roche"]


def check_chunks(text: str, size: int = CHUNK_SIZE, overlap: int = CHUNK_OVERLAP) -> List[TextChunk]:
    """Chunk text and check what holds for any text."""
    chunker = BoundedTextChunker(chunk_size=size, chunk_overlap=overlap)
    chunks = chunker.chunk(text)
    if not text:
        assert chunks == [], chunks
        return chunks
    # The chunker stops with an error past this count: a loop that does not advance fails.
    assert len(chunks) <= chunker.most_chunks(len(text)), (len(chunks), len(text), size, overlap)
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
    """Finds the names as PER, « Besançon » as LOC and « Faible » as a PER of low score.

    Where the text it is given ends or starts within a name, a part of the name is found as
    a PER of a higher score than the whole name: its last words at the start of the text; at
    the end of the text its first word, or its first words without the last one the text
    holds, which then stops short of the end of the text.
    """

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
        for name in NAMES:
            words = name.split(" ")
            for count in range(1, len(words)):
                first, last = " ".join(words[:count]), " ".join(words[count:])
                if text.endswith(first):
                    part = " ".join(words[: max(count - 1, 1)])
                    start = len(text) - len(first)
                    found.append({"entity_group": "PER", "score": 0.999, "start": start, "end": start + len(part)})
                if text.startswith(last):
                    found.append({"entity_group": "PER", "score": 0.999, "start": 0, "end": len(last)})
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

    # A name around a cut is returned once and whole, although the chunk that holds a part
    # of it scores that part higher, whether the part reaches the end of its chunk or not.
    for several_words in NAMES:
        for offset in range(280, 480):
            text = ("ref-0001;" * 60)[:offset] + several_words + ";" + "ref-0002;" * 60
            assert found(recognizer, text) == [("PERSON", several_words)], (several_words, offset)
            text = "Le colis est arrivé. " * 60
            text = text[:offset] + several_words + " " + text
            assert found(recognizer, text) == [("PERSON", several_words)], (several_words, offset)

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


def person(start: int, end: int, score: float, entity_type: str = "PERSON") -> RecognizerResult:
    """A finding with an explanation of its own."""
    explanation = AnalysisExplanation(
        recognizer="check", original_score=score, textual_explanation=f"{start}-{end}"
    )
    return RecognizerResult(
        entity_type=entity_type, start=start, end=end, score=score, analysis_explanation=explanation
    )


def merged(found: List[Tuple[int, RecognizerResult]]) -> List[tuple]:
    return [(r.start, r.end, r.score) for r in BoundedTextChunker.merge_across_chunks(found)]


def reference_merge(found: List[Tuple[int, RecognizerResult]]) -> List[RecognizerResult]:
    """The rule of the merge, written pair by pair: the image's merge must return the same.

    Two findings of one entity type that overlap and are not both of the same single chunk
    are replaced by their union, until no such pair is left.
    """
    groups = [({index}, result) for index, result in found]

    def first_pair():
        for first, (first_chunks, a) in enumerate(groups):
            for second in range(first + 1, len(groups)):
                second_chunks, b = groups[second]
                if (
                    a.entity_type == b.entity_type
                    and a.start < b.end
                    and b.start < a.end
                    and len(first_chunks | second_chunks) > 1
                ):
                    return first, second
        return None

    pair = first_pair()
    while pair:
        first, second = pair
        (first_chunks, a), (second_chunks, b) = groups[first], groups[second]
        best = a if a.score >= b.score else b
        groups[first] = (
            first_chunks | second_chunks,
            RecognizerResult(
                entity_type=best.entity_type,
                start=min(a.start, b.start),
                end=max(a.end, b.end),
                score=best.score,
                analysis_explanation=best.analysis_explanation,
            ),
        )
        del groups[second]
        pair = first_pair()
    return sorted((result for _, result in groups), key=lambda result: result.start)


def described(results: List[RecognizerResult]) -> List[tuple]:
    """What a caller sees of each finding; the explanation by identity."""
    return [(r.entity_type, r.start, r.end, r.score, id(r.analysis_explanation)) for r in results]


def check_merge_against_reference() -> int:
    """The same findings, in the same order, as the rule written pair by pair."""
    rng = random.Random(20261008)
    count = 0
    for distinct_scores in (True, False):
        for _ in range(3000):
            found = []
            for _ in range(rng.randint(0, 14)):
                start = rng.randint(0, 80)
                score = rng.random() if distinct_scores else rng.choice([0.85, 0.9, 0.99])
                entity_type = rng.choice(["PERSON", "PERSON", "LOCATION", "NRP"])
                # Lengths from 1: findings that touch, chains, and several of one chunk.
                found.append((rng.randint(0, 3), person(start, start + rng.randint(1, 25), score, entity_type)))
            results = BoundedTextChunker.merge_across_chunks(found)
            expected = reference_merge(found)
            if distinct_scores:
                assert described(results) == described(expected), found
            else:
                # Of equal scores the explanation kept is the first given: compared without it.
                assert [d[:4] for d in described(results)] == [d[:4] for d in described(expected)], found
            count += 1
    return count


def check_merge_of_many_findings() -> None:
    """Thousands of findings are merged in one sorted pass: this ends, whatever the machine."""
    rng = random.Random(20261009)
    found = []
    for index in range(60000):
        start = rng.randint(0, 3_000_000)
        found.append((index % 9000, person(start, start + rng.randint(1, 30), rng.random())))
    results = BoundedTextChunker.merge_across_chunks(found)
    assert covered(results) == covered([result for _, result in found])
    assert 0 < len(results) <= len(found)


def covered(results: List[RecognizerResult]) -> Dict[str, Set[int]]:
    """The characters the findings designate, per entity type."""
    characters: Dict[str, Set[int]] = {}
    for result in results:
        characters.setdefault(result.entity_type, set()).update(range(result.start, result.end))
    return characters


def check_named_merges() -> None:
    # A part of a name that stops short of the end of its chunk, and the whole name.
    assert merged([(0, person(376, 397, 0.999)), (1, person(376, 404, 0.99))]) == [(376, 404, 0.999)]
    # A long finding and a short one at its tail.
    assert merged([(0, person(340, 400, 0.99)), (1, person(390, 398, 0.9))]) == [(340, 400, 0.99)]
    # Two halves of a name.
    assert merged([(0, person(376, 390, 0.9)), (1, person(385, 404, 0.95))]) == [(376, 404, 0.95)]
    # A chain over three chunks, given in any order.
    chain = [(2, person(28, 40, 0.9)), (0, person(10, 20, 0.97)), (1, person(18, 30, 0.85))]
    assert merged(chain) == [(10, 40, 0.97)]
    # The same finding from two chunks.
    assert merged([(0, person(376, 404, 0.99)), (1, person(376, 404, 0.98))]) == [(376, 404, 0.99)]
    # A finding at the end of its chunk that no other chunk returns is kept.
    assert merged([(0, person(380, 400, 0.9))]) == [(380, 400, 0.9)]
    assert merged([(0, person(380, 400, 0.9)), (1, person(500, 510, 0.9))]) == [(380, 400, 0.9), (500, 510, 0.9)]
    # Neighbours that do not overlap stay two, and so does a person named in two places.
    assert merged([(0, person(380, 390, 0.9)), (1, person(390, 400, 0.95))]) == [(380, 390, 0.9), (390, 400, 0.95)]
    assert merged([(0, person(10, 27, 0.99)), (3, person(1210, 1227, 0.99))]) == [(10, 27, 0.99), (1210, 1227, 0.99)]
    # Overlapping findings that all come from one chunk are left as they are, and so are
    # findings of two entity types.
    assert merged([(0, person(10, 20, 0.9)), (0, person(15, 25, 0.95))]) == [(10, 20, 0.9), (15, 25, 0.95)]
    assert merged([(0, person(10, 20, 0.9)), (1, person(15, 25, 0.95, "LOCATION"))]) == [
        (10, 20, 0.9),
        (15, 25, 0.95),
    ]
    # Two overlapping findings of one chunk become one with a finding of another chunk that
    # overlaps one of them: no character is lost.
    assert merged([(0, person(10, 20, 0.9)), (0, person(15, 25, 0.95)), (1, person(24, 30, 0.8))]) == [(10, 30, 0.95)]

    # A merged finding carries the explanation of the finding of the highest score, and the
    # first given of equal scores.
    part, whole = person(376, 397, 0.999), person(376, 404, 0.99)
    (united,) = BoundedTextChunker.merge_across_chunks([(1, whole), (0, part)])
    assert united.analysis_explanation is part.analysis_explanation
    assert united.entity_type == "PERSON"
    first, second = person(376, 404, 0.99), person(380, 404, 0.99)
    (united,) = BoundedTextChunker.merge_across_chunks([(0, first), (1, second)])
    assert united.analysis_explanation is first.analysis_explanation
    # A finding that is not merged is returned itself.
    alone = person(380, 400, 0.9)
    assert BoundedTextChunker.merge_across_chunks([(0, alone)]) == [alone]

    # Through the chunker: positions are those of the text, not of the chunk.
    text = "x" * 395 + "Corentin Le Guével" + "x" * 300

    def predict(chunk_text: str) -> List[RecognizerResult]:
        position = chunk_text.find("Corentin")
        if position == -1:
            return []
        end = min(position + len("Corentin Le Guével"), len(chunk_text))
        return [person(position, end, 0.9)]

    results = BoundedTextChunker(chunk_size=CHUNK_SIZE, chunk_overlap=CHUNK_OVERLAP).predict_with_chunking(text, predict)
    assert [(r.start, r.end) for r in results] == [(395, 413)], results


def check_random_merges() -> int:
    """No character a chunk designated is dropped, none is added, and nothing is left to merge."""
    rng = random.Random(20261007)
    for _ in range(2000):
        found = []
        for _ in range(rng.randint(0, 12)):
            start = rng.randint(0, 120)
            found.append(
                (
                    rng.randint(0, 3),
                    person(start, start + rng.randint(1, 30), rng.random(), rng.choice(["PERSON", "PERSON", "LOCATION"])),
                )
            )
        results = BoundedTextChunker.merge_across_chunks(found)
        assert covered(results) == covered([result for _, result in found]), found
        assert [r.start for r in results] == sorted(r.start for r in results)
        for entity_type in ("PERSON", "LOCATION"):
            given = [(chunk, r) for chunk, r in found if r.entity_type == entity_type]
            kept = [r for r in results if r.entity_type == entity_type]
            if given:
                assert max(r.score for r in kept) == max(r.score for _, r in given)
            for index, a in enumerate(kept):
                for b in kept[index + 1 :]:
                    if a.start < b.end and b.start < a.end:
                        # What still overlaps is two findings of one chunk, as it returned them.
                        assert any(
                            {(a.start, a.end, a.score), (b.start, b.end, b.score)}
                            <= {(r.start, r.end, r.score) for chunk, r in given if chunk == index_of_chunk}
                            for index_of_chunk in range(4)
                        ), (a, b, found)
    return 2000


if __name__ == "__main__":
    random_texts = check_random_texts()
    names = check_names_in_text_with_little_whitespace()
    check_named_texts()
    check_named_merges()
    merges = check_random_merges()
    compared = check_merge_against_reference()
    check_merge_of_many_findings()
    check_predictions()
    print(
        f"recognizer checks passed: {random_texts} random texts, {names} name positions, "
        f"{merges} random merges, {compared} merges compared with the reference"
    )
