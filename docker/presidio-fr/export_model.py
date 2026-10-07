"""Export a token classification model for the recognizer of this image. Build stage only.

    python export_model.py <source directory> <output directory> <repository@revision>

The source directory holds the model as its repository publishes it. The output directory
receives what `onnx_ner_recognizer.py` loads:

- `model.onnx`: the model exported to ONNX, its weights quantized to 8 bits;
- `tokenizer.json`: the tokenizer, readable without transformers;
- `ner.json`: where the model comes from, its labels and the tokens it reads at once.

The export is checked before it is kept: the quantized model must label the tokens of the
check sentences as the model it comes from does.
"""

import json
import sys
from pathlib import Path

import numpy as np
import onnxruntime
import torch
from onnxruntime.quantization import QuantType, quantize_dynamic
from tokenizers import Tokenizer
from transformers import AutoModelForTokenClassification, AutoTokenizer

OPSET = 17
MAX_TOKENS = 512
# Sentences of the check: persons, a company and a city, a sentence that names nobody.
SENTENCES = [
    "Bonjour, je suis Hélène Marchand.",
    "Hélène Marchand travaille chez Batiloire à Besançon.",
    "Rappeler M. Vasseur avant jeudi, vu avec Clémence Aubry.",
    "Commande de Tondeuse Verdia livrée par Batiloire.",
]
# The share of tokens the quantized model may label otherwise than the model it comes from.
MOST_DIFFERING_TOKENS = 0.0


def main(source: Path, output: Path, origin: str) -> None:
    output.mkdir(parents=True, exist_ok=True)
    exported = output / "exported.onnx"
    quantized = output / "model.onnx"

    tokenizer = AutoTokenizer.from_pretrained(source)
    assert tokenizer.is_fast, "a fast tokenizer is needed: it gives the positions of the tokens"
    assert tokenizer.model_max_length == MAX_TOKENS, tokenizer.model_max_length
    model = AutoModelForTokenClassification.from_pretrained(source).eval()
    labels = [model.config.id2label[index] for index in range(model.config.num_labels)]

    sample = tokenizer(SENTENCES[:2], return_tensors="pt", padding=True)
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

    check(model, output, labels)


def check(model, output: Path, labels: list) -> None:
    """Compare the labels of the exported model with those of its source, token by token."""
    light = Tokenizer.from_file(str(output / "tokenizer.json"))
    light.no_truncation()
    light.no_padding()
    session = onnxruntime.InferenceSession(
        str(output / "model.onnx"), providers=["CPUExecutionProvider"]
    )
    tokens = differing = persons = 0
    for sentence in SENTENCES:
        encoding = light.encode(sentence)
        ids = np.array([encoding.ids], dtype=np.int64)
        mask = np.ones_like(ids)
        quantized = session.run(["logits"], {"input_ids": ids, "attention_mask": mask})[0][0]
        with torch.inference_mode():
            reference = model(input_ids=torch.from_numpy(ids), attention_mask=torch.from_numpy(mask))[0][0]
        got = quantized.argmax(axis=-1)
        expected = reference.numpy().argmax(axis=-1)
        tokens += len(got)
        differing += int((got != expected).sum())
        persons += sum(1 for index in got if labels[index].endswith("PER"))
    assert persons > 0, "the exported model labels no token as a person"
    assert differing <= MOST_DIFFERING_TOKENS * tokens, (
        f"{differing} of {tokens} tokens are labelled otherwise by the exported model"
    )
    print(f"export checked: {tokens} tokens, {differing} labelled otherwise, {persons} of persons")


if __name__ == "__main__":
    main(Path(sys.argv[1]), Path(sys.argv[2]), sys.argv[3])
