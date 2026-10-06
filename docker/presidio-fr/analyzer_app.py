"""Presidio's own server, with the recognizer class of this image known to its registry.

A registry file can only name a recognizer class that is already imported: this module
imports it, then hands over to Presidio's app unchanged.
"""

import mapped_ner_recognizer  # noqa: F401  (registers the class)
from app import create_app  # noqa: F401
