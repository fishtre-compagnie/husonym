#!/bin/sh
set -e

# Fetch an LLM model (GGUF format) from HuggingFace
# Usage: ./scripts/fetch-llm-model.sh [MODEL_URL] [SHA256]
# Environment variables:
#   MODEL_URL - Full URL to the GGUF model file on HuggingFace (or any HTTPS source)
#   SHA256 - Expected SHA256 checksum of the downloaded file (optional)

# Default to Qwen3-4B-Instruct Q4_K_M quantization
# Model source: https://huggingface.co/Qwen/Qwen3-4B-Instruct-GGUF
MODEL_URL="${1:-https://huggingface.co/bartowski/Qwen3-4B-Instruct-GGUF/resolve/main/Qwen3-4B-Instruct-Q4_K_M.gguf?download=true}"
SHA256="${2:-}"

# SHA256 placeholder - update with actual checksum once model is finalized
# To compute: sha256sum ./llm-models/model.gguf
EXPECTED_SHA256="${SHA256:-}"

OUTPUT_DIR="./llm-models"
FILENAME=$(basename "${MODEL_URL}" | cut -d'?' -f1)  # Strip query parameters
OUTPUT_FILE="${OUTPUT_DIR}/${FILENAME}"

echo "Fetching LLM model..."
echo "URL: ${MODEL_URL}"
echo "Output: ${OUTPUT_FILE}"

mkdir -p "${OUTPUT_DIR}"

# Download with curl, following redirects and showing progress
curl -L --progress-bar "${MODEL_URL}" -o "${OUTPUT_FILE}"

if [ -f "${OUTPUT_FILE}" ]; then
  echo "Downloaded: ${OUTPUT_FILE} ($(du -h "${OUTPUT_FILE}" | cut -f1))"

  # Verify SHA256 if provided
  if [ -n "${EXPECTED_SHA256}" ]; then
    echo "Verifying SHA256 checksum..."
    ACTUAL_SHA256=$(sha256sum "${OUTPUT_FILE}" | cut -d' ' -f1)

    if [ "${ACTUAL_SHA256}" = "${EXPECTED_SHA256}" ]; then
      echo "Checksum verified successfully."
    else
      echo "ERROR: Checksum mismatch!"
      echo "Expected: ${EXPECTED_SHA256}"
      echo "Actual:   ${ACTUAL_SHA256}"
      rm "${OUTPUT_FILE}"
      exit 1
    fi
  else
    echo "No checksum provided; skipping verification."
    echo "To verify in future, compute and pass SHA256:"
    ACTUAL_SHA256=$(sha256sum "${OUTPUT_FILE}" | cut -d' ' -f1)
    echo "  SHA256=${ACTUAL_SHA256}"
  fi
else
  echo "ERROR: Failed to download model"
  exit 1
fi
