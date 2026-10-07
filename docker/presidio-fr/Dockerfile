# Presidio Analyzer + français : modèle spaCy fr_core_news_md, et un modèle DistilCamemBERT
# quantifié, exécuté par ONNX Runtime, pour les personnes.
#
# L'image officielle n'embarque que en_core_web_lg : sans modèle français, toute requête
# avec language=fr est rejetée (« No matching recognizers were found »).
#
# Deux étapes. La première (« model ») télécharge le modèle, l'exporte en ONNX et le quantifie :
# elle a besoin de PyTorch et de transformers, qui n'en sortent pas. La seconde est l'image :
# elle ne reçoit de la première que le dossier du modèle exporté.
#
# La construction a besoin du réseau, au-delà du registre de l'image de base :
#   - PyPI : pypi.org pour l'index, files.pythonhosted.org pour les fichiers ;
#   - PyTorch : download.pytorch.org pour l'index, download-r2.pytorch.org pour les fichiers ;
#   - modèle spaCy : github.com, qui redirige vers release-assets.githubusercontent.com ;
#   - modèle des personnes : huggingface.co, et pour les poids les hôtes de fichiers de Hugging
#     Face sous hf.co (cas-server.xethub.hf.co avec le client hf_xet installé à la première
#     étape ; us.aws.cdn.hf.co par la redirection de huggingface.co sans lui).
# Ces hôtes sont ceux observés à une construction ; ceux des fichiers peuvent changer.
# L'image, elle, ne joint jamais le réseau : tout ce qu'elle charge est dans ses couches.
#
# Image de base épinglée par étiquette ET par empreinte (voir le README pour la faire évoluer).
# Les deux étapes partent de la même, et lisent le modèle avec les mêmes versions d'ONNX Runtime
# et de tokenizers.
ARG BASE_IMAGE=mcr.microsoft.com/presidio-analyzer:2.2.362@sha256:286e3fa7f3a7426e775e8564fe1870f1ba8f999d3ab8bbb8cc46a44355d9d6e9
ARG ONNXRUNTIME_VERSION=1.30.0
ARG TOKENIZERS_VERSION=0.23.2

# --- Étape 1 : le modèle des personnes, exporté et quantifié ---------------------------------
FROM ${BASE_IMAGE} AS model
USER root

# PyTorch en version CPU seule (aucune bibliothèque CUDA), transformers et ce dont il a besoin
# pour convertir le tokenizer (sentencepiece, protobuf), onnx pour l'export et la quantification.
ARG TORCH_VERSION=2.14.1
ARG TRANSFORMERS_VERSION=5.19.0
ARG SENTENCEPIECE_VERSION=0.2.2
ARG PROTOBUF_VERSION=7.36.2
ARG ONNX_VERSION=1.23.2
ARG ONNXRUNTIME_VERSION
ARG TOKENIZERS_VERSION
RUN pip install --no-cache-dir --index-url https://download.pytorch.org/whl/cpu "torch==${TORCH_VERSION}+cpu" \
 && pip install --no-cache-dir "transformers==${TRANSFORMERS_VERSION}" \
      "sentencepiece==${SENTENCEPIECE_VERSION}" "protobuf==${PROTOBUF_VERSION}" \
      "onnx==${ONNX_VERSION}" "onnxruntime==${ONNXRUNTIME_VERSION}" "tokenizers==${TOKENIZERS_VERSION}"

# Le modèle, à une révision épinglée par commit, dans un seul format de poids (safetensors).
ARG NER_REPO=cmarkea/distilcamembert-base-ner
ARG NER_REVISION=e539d952f70088b450c300c28ba455da87f2dc4b
# Empreinte SHA-256 des poids (model.safetensors) à cette révision, celle que publie Hugging Face
# pour ce fichier : des poids qui en diffèrent arrêtent la construction.
ARG NER_WEIGHTS_SHA256=3e05cfc8538eda6bf0aa6932533c0533cbf063ddc245705ccb421ebb9a8da97c
RUN python -c "from huggingface_hub import snapshot_download; \
snapshot_download(repo_id='${NER_REPO}', revision='${NER_REVISION}', local_dir='/build/source', \
allow_patterns=['config.json','model.safetensors','sentencepiece.bpe.model','tokenizer_config.json','README.md'])" \
 && echo "${NER_WEIGHTS_SHA256}  /build/source/model.safetensors" | sha256sum -c -

# L'export, réseau interdit aux bibliothèques de Hugging Face : les fichiers téléchargés suffisent.
# Le script compare, avant de le garder, le modèle quantifié à celui dont il vient (voir
# export_model.py). La fiche du modèle, qui porte sa licence, suit le modèle dans l'image.
COPY export_model.py /build/
RUN HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 python -B /build/export_model.py \
      /build/source /build/model "${NER_REPO}@${NER_REVISION}" \
 && cp /build/source/README.md /build/model/README.md

# --- Étape 2 : l'image -----------------------------------------------------------------------
FROM ${BASE_IMAGE}
USER root

ARG SPACY_FR_MODEL=fr_core_news_md-3.8.0
RUN poetry run python -m spacy download "${SPACY_FR_MODEL}" --direct

# Ce qui exécute le modèle : ONNX Runtime, et tokenizers pour découper le texte. Ni PyTorch ni
# transformers.
ARG ONNXRUNTIME_VERSION
ARG TOKENIZERS_VERSION
RUN pip install --no-cache-dir "onnxruntime==${ONNXRUNTIME_VERSION}" "tokenizers==${TOKENIZERS_VERSION}"

COPY --from=model --chown=presidio:presidio /build/model /app/models/distilcamembert-ner

# ONNX Runtime dimensionne son pool de threads sur les cœurs de l'hôte, pas sur le quota CPU du
# conteneur : sous un quota, les threads en trop se disputent le temps alloué. Le reconnaisseur
# de l'image lit cette variable pour fixer le nombre de threads d'une inférence. Deux par
# défaut ; à surcharger selon le quota réellement donné (voir le README).
ENV OMP_NUM_THREADS=2
# Durée, en secondes, au bout de laquelle gunicorn tue un worker qui n'a pas fini sa requête
# (son option --timeout, 30 par défaut). Un worker tué répond par une erreur 500 et son
# remplaçant recharge les modèles. 120 par défaut : au-dessus des 60 s qu'attend le client de
# Husonym, pour que ce soit la limite du client que rencontre un appelant (voir le README).
ENV WORKER_TIMEOUT=120

# La configuration vit dans l'image. Les trois fichiers vont ensemble : le moteur d'analyse refuse
# de démarrer si ses langues ne correspondent pas à celles du registre.
COPY --chown=presidio:presidio nlp.yaml recognizers.yaml analyzer.yaml /app/conf/fr/
ENV NLP_CONF_FILE=/app/conf/fr/nlp.yaml \
    RECOGNIZER_REGISTRY_CONF_FILE=/app/conf/fr/recognizers.yaml \
    ANALYZER_CONF_FILE=/app/conf/fr/analyzer.yaml

# Le reconnaisseur propre à cette image, et le serveur de Presidio lancé par un module qui
# l'importe. Même commande que l'entrypoint.sh de l'image de base, au nom du module près et aux
# options --no-control-socket et --timeout près (voir la commande, en bas).
COPY --chown=presidio:presidio onnx_ner_recognizer.py analyzer_app.py recognizer_check.py server_check.py /app/
# Les contrôles du reconnaisseur (découpage, réunion des trouvailles de deux morceaux,
# regroupement des tokens, filtre des étiquettes, erreur d'inférence), sans le modèle : un
# contrôle qui échoue, ou un nom interne de Presidio qui manque, arrête la construction. Le
# fichier reste dans l'image (voir le README).
RUN python -B /app/recognizer_check.py

# Utilisateur non privilégié de l'image de base (presidio).
USER 1001
# Le contrôle du serveur, avec le vrai modèle, sous l'utilisateur de l'image : l'application est
# créée comme gunicorn la crée, par le registre et les trois fichiers de configuration, une
# phrase française lui est envoyée, et la personne doit revenir du reconnaisseur de cette image,
# sans type d'entité hors de la liste déclarée. Un nom de Presidio, d'ONNX Runtime ou de
# tokenizers qui a changé arrête ici la construction.
RUN python -B /app/server_check.py
# --no-control-socket : gunicorn 25.1.0 (celui de l'image de base) ouvre un socket de contrôle
# dans un thread juste avant de créer son worker par fork. Si le fork tombe pendant que ce thread
# écrit dans le journal, le worker hérite d'un verrou que personne ne relâchera : il ne démarre
# jamais et /health ne répond pas. Défaut décrit dans l'issue 3529 de github.com/benoitc/gunicorn
# et corrigé par sa pull request 3520, dans gunicorn 25.2.0. Ce socket ne sert pas ici.
# À retirer quand l'image de base embarquera gunicorn 25.2.0 ou plus.
# --timeout : voir WORKER_TIMEOUT, plus haut.
CMD ["sh", "-c", "exec poetry run gunicorn --no-control-socket --timeout \"$WORKER_TIMEOUT\" -w \"$WORKERS\" -b \"0.0.0.0:$PORT\" \"analyzer_app:create_app()\""]
