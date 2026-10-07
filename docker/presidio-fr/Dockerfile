# Presidio Analyzer + français : modèle spaCy fr_core_news_md, et CamemBERT pour les personnes.
#
# L'image officielle n'embarque que en_core_web_lg : sans modèle français, toute requête
# avec language=fr est rejetée (« No matching recognizers were found »).
#
# La construction a besoin du réseau (PyPI, download.pytorch.org, github.com, huggingface.co) ;
# l'image, elle, ne le joint jamais : tout ce qu'elle charge est dans ses couches.
#
# Image de base épinglée par étiquette ET par empreinte (voir le README pour la faire évoluer).
FROM mcr.microsoft.com/presidio-analyzer:2.2.362@sha256:286e3fa7f3a7426e775e8564fe1870f1ba8f999d3ab8bbb8cc46a44355d9d6e9
USER root

ARG SPACY_FR_MODEL=fr_core_news_md-3.8.0
RUN poetry run python -m spacy download "${SPACY_FR_MODEL}" --direct

# PyTorch en version CPU seule (aucune bibliothèque CUDA), puis transformers et ce dont il a
# besoin pour convertir le tokenizer de CamemBERT (sentencepiece, protobuf).
ARG TORCH_VERSION=2.14.1
ARG TRANSFORMERS_VERSION=5.19.0
ARG SENTENCEPIECE_VERSION=0.2.2
ARG PROTOBUF_VERSION=7.36.2
RUN pip install --no-cache-dir --index-url https://download.pytorch.org/whl/cpu "torch==${TORCH_VERSION}+cpu" \
 && pip install --no-cache-dir "transformers==${TRANSFORMERS_VERSION}" \
      "sentencepiece==${SENTENCEPIECE_VERSION}" "protobuf==${PROTOBUF_VERSION}"

# Le modèle, à une révision épinglée par commit, dans un seul format de poids (safetensors).
# Téléchargement, preuve et changement de propriétaire dans UNE couche : un chown dans une couche
# suivante stockerait les poids deux fois.
# La preuve tourne réseau interdit (HF_HUB_OFFLINE) : les fichiers embarqués suffisent. Le
# tokenizer rapide y est écrit une fois (le dépôt ne livre que le fichier sentencepiece), ce qui
# évite de refaire la conversion à chaque démarrage ; il est requis pour les positions.
ARG CAMEMBERT_REPO=Jean-Baptiste/camembert-ner
ARG CAMEMBERT_REVISION=ef35fe7767c1dad71f5c853838cdd80d0b3441ed
RUN python -c "from huggingface_hub import snapshot_download; \
snapshot_download(repo_id='${CAMEMBERT_REPO}', revision='${CAMEMBERT_REVISION}', \
local_dir='/app/models/camembert-ner', \
allow_patterns=['config.json','model.safetensors','sentencepiece.bpe.model','special_tokens_map.json','tokenizer_config.json','README.md'])" \
 && rm -rf /app/models/camembert-ner/.cache /root/.cache/huggingface \
 && HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 python -c "from transformers import AutoTokenizer, pipeline; \
p='/app/models/camembert-ner'; t=AutoTokenizer.from_pretrained(p); assert t.is_fast, 'fast tokenizer needed for offsets'; \
assert t.model_max_length == 512, t.model_max_length; t.save_pretrained(p); \
n=pipeline('token-classification', model=p, tokenizer=p, aggregation_strategy='simple', device=-1); \
r=n('Bonjour, je suis Hélène Marchand.'); print(r); assert [e['entity_group'] for e in r]==['PER'], r" \
 && echo "${CAMEMBERT_REPO}@${CAMEMBERT_REVISION}" > /app/models/camembert-ner/REVISION \
 && chown -R presidio:presidio /app/models

# En fonctionnement, rien ne joint le réseau.
ENV HF_HUB_OFFLINE=1 \
    TRANSFORMERS_OFFLINE=1 \
    HF_HUB_DISABLE_TELEMETRY=1
# PyTorch dimensionne son pool de threads sur les cœurs de l'hôte, pas sur le quota CPU du
# conteneur : sous un quota, les threads en trop se disputent le temps alloué. Deux threads par
# défaut ; à surcharger selon le quota réellement donné (voir le README).
ENV OMP_NUM_THREADS=2

# La configuration vit dans l'image. Les trois fichiers vont ensemble : le moteur d'analyse refuse
# de démarrer si ses langues ne correspondent pas à celles du registre.
COPY --chown=presidio:presidio nlp.yaml recognizers.yaml analyzer.yaml /app/conf/fr/
ENV NLP_CONF_FILE=/app/conf/fr/nlp.yaml \
    RECOGNIZER_REGISTRY_CONF_FILE=/app/conf/fr/recognizers.yaml \
    ANALYZER_CONF_FILE=/app/conf/fr/analyzer.yaml

# Le reconnaisseur propre à cette image, et le serveur de Presidio lancé par un module qui
# l'importe. Même commande que l'entrypoint.sh de l'image de base, au nom du module près et à
# l'option --no-control-socket près (voir la commande, en bas).
COPY --chown=presidio:presidio mapped_ner_recognizer.py analyzer_app.py recognizer_check.py /app/
# Les contrôles du reconnaisseur (découpage, réunion des trouvailles de deux morceaux, filtre des
# étiquettes, erreur d'inférence), sans le modèle : un contrôle qui échoue, ou un nom interne de
# Presidio qui manque, arrête la construction. Le fichier reste dans l'image (voir le README).
RUN python -B /app/recognizer_check.py

# Utilisateur non privilégié de l'image de base (presidio).
USER 1001
# --no-control-socket : gunicorn 25.1.0 (celui de l'image de base) ouvre un socket de contrôle
# dans un thread juste avant de créer son worker par fork. Si le fork tombe pendant que ce thread
# écrit dans le journal, le worker hérite d'un verrou que personne ne relâchera : il ne démarre
# jamais et /health ne répond pas (gunicorn #3509, corrigé en 25.2.0). Ce socket ne sert pas ici.
# À retirer quand l'image de base embarquera gunicorn 25.2.0 ou plus.
CMD ["sh", "-c", "exec poetry run gunicorn --no-control-socket -w \"$WORKERS\" -b \"0.0.0.0:$PORT\" \"analyzer_app:create_app()\""]
