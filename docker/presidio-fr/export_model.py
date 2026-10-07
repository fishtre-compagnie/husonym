"""Export a token classification model for the recognizer of this image. Build stage only.

    python export_model.py <source directory> <output directory> <repository@revision>

The source directory holds the model as its repository publishes it. The output directory
receives what `onnx_ner_recognizer.py` loads:

- `model.onnx`: the model exported to ONNX, its weights quantized to 8 bits;
- `tokenizer.json`: the tokenizer, readable without transformers;
- `ner.json`: where the model comes from, its labels and the tokens it reads at once.

The export is checked before it is kept, on a few sentences: the written tokenizer must give
the tokens of its source, and the quantized model must label them as the model it comes from
does, with close probabilities. It is a check of the export, not a measure of the model.
"""

import json
import os
import sys
from pathlib import Path

# The official builds of ONNX Runtime report usage events to their publisher over HTTPS,
# unless this variable is set before the library initializes.
os.environ["ORT_DISABLE_TELEMETRY"] = "1"

import numpy as np  # noqa: E402
import onnxruntime  # noqa: E402
import torch  # noqa: E402
from onnxruntime.quantization import QuantType, quantize_dynamic  # noqa: E402
from tokenizers import Tokenizer  # noqa: E402
from transformers import AutoModelForTokenClassification, AutoTokenizer  # noqa: E402

OPSET = 17
MAX_TOKENS = 512
# Sentences of the check: persons, a company and a city, accents and characters of several
# tokens, a sentence that names nobody.
SENTENCES = [
    "Bonjour, je suis Hélène Marchand.",
    "Hélène Marchand travaille chez Batiloire à Besançon.",
    "Rappeler M. Vasseur avant jeudi, vu avec Clémence Aubry.",
    "Commande de Tondeuse Verdia livrée par Batiloire.",
    "Le dossier n° 4 (½ journée, cœur de réseau) est suivi par Anne-Sophie de Villeroy.",
    "Relance faite ce matin, pas de réponse du client.",
]
# The share of tokens the quantized model may label otherwise than the model it comes from.
MOST_DIFFERING_TOKENS = 0.0
# How far the probability the quantized model gives a token's label may be from the one the
# model it comes from gives it, special tokens aside. The recognizer keeps an entity on the
# mean of these. Observed on the check sentences: 0.22.
LARGEST_PROBABILITY_GAP = 0.3


def main(source: Path, output: Path, origin: str) -> None:
    output.mkdir(parents=True, exist_ok=True)
    exported = output / "exported.onnx"
    quantized = output / "model.onnx"

    tokenizer = AutoTokenizer.from_pretrained(source)
    assert tokenizer.is_fast, "a fast tokenizer is needed: it gives the positions of the tokens"
    assert tokenizer.model_max_length == MAX_TOKENS, tokenizer.model_max_length
    model = AutoModelForTokenClassification.from_pretrained(source).eval()
    labels = [model.config.id2label[index] for index in range(model.config.num_labels)]

    # One sentence, without padding: the tokenizer is written as it is, with no setting
    # of a batch left in it.
    sample = tokenizer(SENTENCES[0], return_tensors="pt")
    model.config.return_dict = False
    torch.onnx.export(
        model,
        (sample["input_ids"], sample["attention_mask"]),
        str(exported),
        input_names=["input_ids", "attention_mask"],
        output_names=["logits"],
        dynamic_axes={
            "input_ids": {0: "batch", 1: "tokens"},
            "attention_mask": {0: "batch", 1: "tokens"},
            "logits": {0: "batch", 1: "tokens"},
        },
        opset_version=OPSET,
        dynamo=False,
    )
    # One scale per output channel of each weight: with a single scale per weight this
    # model stops finding names. A reduced range keeps the 8-bit products from saturating
    # on processors without the VNNI instructions.
    quantize_dynamic(
        str(exported), str(quantized), weight_type=QuantType.QInt8, per_channel=True, reduce_range=True
    )
    exported.unlink()

    tokenizer.save_pretrained(output)
    for name in output.iterdir():
        if name.name not in ("model.onnx", "tokenizer.json"):
            name.unlink()
    (output / "ner.json").write_text(
        json.dumps({"source": origin, "labels": labels, "max_tokens": MAX_TOKENS}, indent=2) + "\n",
        encoding="utf-8",
    )

    check(model, tokenizer, output, labels)


def probabilities(logits: np.ndarray) -> np.ndarray:
    exponentials = np.exp(logits - logits.max(axis=-1, keepdims=True))
    return exponentials / exponentials.sum(axis=-1, keepdims=True)


def check(model, tokenizer, output: Path, labels: list) -> None:
    """Compare what was written with the model and the tokenizer it comes from.

    On the check sentences: the written tokenizer gives the tokens of its source; the
    quantized model gives each token the label its source gives it, with a probability
    close to its source's.
    """
    light = Tokenizer.from_file(str(output / "tokenizer.json"))
    assert light.padding is None and light.truncation is None, (light.padding, light.truncation)
    session = onnxruntime.InferenceSession(
        str(output / "model.onnx"), providers=["CPUExecutionProvider"]
    )
    tokens = differing = persons = 0
    largest_gap = 0.0
    for sentence in SENTENCES:
        encoding = light.encode(sentence)
        assert encoding.ids == tokenizer(sentence)["input_ids"], f"tokens differ for {sentence!r}"
        ids = np.array([encoding.ids], dtype=np.int64)
        mask = np.ones_like(ids)
        quantized = session.run(["logits"], {"input_ids": ids, "attention_mask": mask})[0][0]
        with torch.inference_mode():
            reference = model(input_ids=torch.from_numpy(ids), attention_mask=torch.from_numpy(mask))[0][0]
        got, expected = probabilities(quantized), probabilities(reference.numpy())
        label = expected.argmax(axis=-1)
        chosen = np.arange(len(label))
        tokens += len(label)
        differing += int((got.argmax(axis=-1) != label).sum())
        persons += sum(1 for index in label if labels[index].endswith("PER"))
        gaps = np.abs(got[chosen, label] - expected[chosen, label])
        largest_gap = max(largest_gap, float(gaps[np.array(encoding.special_tokens_mask) == 0].max()))
    assert persons > 0, "the model labels no token of the check sentences as a person"
    assert differing <= MOST_DIFFERING_TOKENS * tokens, (
        f"{differing} of {tokens} tokens are labelled otherwise by the exported model"
    )
    assert largest_gap <= LARGEST_PROBABILITY_GAP, (
        f"the probability of a label differs by {largest_gap:.3f} in the exported model"
    )
    print(
        f"export checked: {tokens} tokens, {differing} labelled otherwise, {persons} of persons, "
        f"largest probability gap {largest_gap:.3f}"
    )


if __name__ == "__main__":
    main(Path(sys.argv[1]), Path(sys.argv[2]), sys.argv[3])
