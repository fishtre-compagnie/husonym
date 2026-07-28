#!/usr/bin/env bash
# Régénère le parseur T-SQL à partir de la grammaire (grammar/*.g4).
#
# Le code Go de ce package est GÉNÉRÉ — ne pas l'éditer à la main.
# Pour le régénérer, lancer ce script depuis n'importe quel répertoire.
#
# Prérequis : Docker (aucune installation de Java requise sur l'hôte).
set -euo pipefail

ANTLR_VERSION="4.13.2"
PKG="tsqlparser"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
JAR="$DIR/antlr-${ANTLR_VERSION}-complete.jar"

# Télécharge le jar ANTLR si absent (non versionné).
if [[ ! -f "$JAR" ]]; then
  curl -fsSL "https://www.antlr.org/download/antlr-${ANTLR_VERSION}-complete.jar" -o "$JAR"
fi

# Génère le lexer + parser + listener en Go via un conteneur Java.
docker run --rm \
  -v "$DIR/grammar:/grammar" \
  -v "$JAR:/antlr.jar" \
  -v "$DIR:/out" \
  -w /grammar \
  eclipse-temurin:21-jdk \
  java -jar /antlr.jar -Dlanguage=Go -package "$PKG" -listener -o /out \
    TSqlLexer.g4 TSqlParser.g4

# Corrige la propriété des fichiers écrits par le conteneur (root).
sudo chown -R "$(id -u):$(id -g)" "$DIR" 2>/dev/null || chown -R "$(id -u):$(id -g)" "$DIR"

# Supprime les artefacts inutiles au runtime (tooling only).
rm -f "$DIR"/*.interp "$DIR"/*.tokens

echo "Parseur T-SQL régénéré dans $DIR"
