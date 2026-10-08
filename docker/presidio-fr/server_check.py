"""Check of the server as it starts in the image: the registry builds the recognizer, model loaded.

The image build runs this file after the configuration and the modules are in place: the
application is created as gunicorn creates it, one French sentence is posted to it, and the
person must come back from the recognizer of this image. A name of Presidio, of ONNX Runtime
or of tokenizers that changed fails here, and so fails the build.

ONNX Runtime must also have left nothing in the user's directory: with its usage reporting
on, it writes an identifier and a queue of events there, which the image would then carry.
"""

from pathlib import Path

from analyzer_app import create_app

# Where ONNX Runtime keeps the identifier and the events of its usage reporting.
USAGE_REPORTING_DIRECTORY = Path.home() / ".cache" / "Microsoft"

DECLARED_ENTITIES = {
    "CREDIT_CARD", "CRYPTO", "DATE_TIME", "EMAIL_ADDRESS", "FR_NIR", "FR_PHONE_NUMBER",
    "FR_POSTAL_CODE", "FR_SIRET", "IBAN_CODE", "IP_ADDRESS", "LOCATION", "MAC_ADDRESS", "NRP",
    "PERSON", "PHONE_NUMBER", "URL",
}  # fmt: skip
PERSON_RECOGNIZER = "OnnxNerRecognizer"
PERSON_THRESHOLD = 0.6
# The model labels the company and the city too: only the person is mapped.
TEXT = "Hélène Marchand travaille chez Batiloire à Besançon."
NAME = "Hélène Marchand"


def check_french_person() -> list:
    client = create_app().test_client()
    response = client.post(
        "/analyze",
        json={"text": TEXT, "language": "fr", "return_decision_process": True},
    )
    assert response.status_code == 200, (response.status_code, response.get_data(as_text=True))
    findings = response.get_json()

    undeclared = {finding["entity_type"] for finding in findings} - DECLARED_ENTITIES
    assert not undeclared, (undeclared, findings)

    persons = [finding for finding in findings if finding["entity_type"] == "PERSON"]
    assert len(persons) == 1, findings
    person = persons[0]
    assert TEXT[person["start"] : person["end"]] == NAME, person
    assert person["analysis_explanation"]["recognizer"] == PERSON_RECOGNIZER, person
    assert PERSON_THRESHOLD < person["score"] < 1, person
    return findings


if __name__ == "__main__":
    found = check_french_person()
    assert not USAGE_REPORTING_DIRECTORY.exists(), sorted(USAGE_REPORTING_DIRECTORY.rglob("*"))
    print(
        "server check passed: "
        + ", ".join(f"{f['entity_type']} {TEXT[f['start']:f['end']]!r} {f['score']:.2f}" for f in found)
    )
