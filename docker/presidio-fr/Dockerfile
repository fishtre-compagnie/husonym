# Presidio Analyzer + modele spaCy francais.
# L'image officielle n'embarque que en_core_web_lg : sans modele fr, toute requete
# avec language=fr est rejetee ("No matching recognizers were found").
FROM mcr.microsoft.com/presidio-analyzer:latest
USER root
RUN poetry run python -m spacy download fr_core_news_md
