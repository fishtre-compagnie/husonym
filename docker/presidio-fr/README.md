# Presidio Analyzer francisé

Image dérivée de `mcr.microsoft.com/presidio-analyzer`, ajoutant le français : un
catalogue de reconnaisseurs français, le modèle spaCy `fr_core_news_md`, et
CamemBERT pour reconnaître les personnes.

## Pourquoi

L'image officielle n'embarque que le modèle anglais `en_core_web_lg`, et sur ses
50 reconnaisseurs pays (US, UK, Espagne, Italie, Inde, Australie, Corée,
Pologne…) **aucun ne couvre la France**. Conséquences mesurées sur des données
françaises réelles :

- `0710203040` (téléphone) : **rien détecté** ;
- `+33 7 10 20 30 40` : classé **DATE_TIME** ;
- `180017511600146` (NIR) : classé **CREDIT_CARD avec un score de 1.00** — il
  passe Luhn par hasard ;
- villes françaises reconnues sur 12 valeurs sur 24.

Mesuré au banc `scripts/testdata/bench-presidio.py`, le passage au catalogue
français fait progresser le F1 de **0,72 à 0,80** (rappel 71 % → 77 %,
précision 73 % → 83 %) et supprime le seul faux positif.

## Contenu

| Fichier | Rôle |
|---|---|
| `Dockerfile` | ajoute `fr_core_news_md`, PyTorch (CPU), transformers et le modèle CamemBERT ; copie la configuration et fixe les variables qui la désignent |
| `nlp.yaml` | moteur NLP anglais + français (spaCy), avec le mapping d'entités spaCy fr (PER/LOC…) |
| `recognizers.yaml` | catalogue en + fr : reconnaisseurs français absents de Presidio, et reconnaisseur des personnes en français |
| `analyzer.yaml` | langues du moteur d'analyse |
| `mapped_ner_recognizer.py` | le reconnaisseur des personnes en français (sous-classe de celui de Presidio) |
| `analyzer_app.py` | point d'entrée du serveur : importe le reconnaisseur, puis lance l'application de Presidio |

**Les trois fichiers de configuration sont requis ensemble.** Le moteur refuse
de démarrer si ses langues ne correspondent pas exactement à celles du registre
de reconnaisseurs — d'où `analyzer.yaml`, facile à oublier. Ils sont copiés dans
l'image (`/app/conf/fr/`) et les variables `NLP_CONF_FILE`,
`RECOGNIZER_REGISTRY_CONF_FILE` et `ANALYZER_CONF_FILE` y sont fixées : rien à
monter ni à déclarer au lancement. Après une modification, reconstruire l'image.

Les entités déclarées (`/supportedentities`) sont les mêmes seize dans les deux
langues : `CREDIT_CARD`, `CRYPTO`, `DATE_TIME`, `EMAIL_ADDRESS`, `FR_NIR`,
`FR_PHONE_NUMBER`, `FR_POSTAL_CODE`, `FR_SIRET`, `IBAN_CODE`, `IP_ADDRESS`,
`LOCATION`, `MAC_ADDRESS`, `NRP`, `PERSON`, `PHONE_NUMBER`, `URL`.

## Personnes en français : CamemBERT

spaCy reste le moteur NLP des deux langues. En anglais rien ne change : les
personnes viennent du `SpacyRecognizer`. En français :

- le `SpacyRecognizer` ne rend plus que `LOCATION`, `DATE_TIME` et `NRP` ;
- les personnes viennent de `MappedLabelsNerRecognizer`, qui interroge le modèle
  [`Jean-Baptiste/camembert-ner`](https://huggingface.co/Jean-Baptiste/camembert-ner)
  embarqué dans l'image (`/app/models/camembert-ner`), avec l'agrégation
  `simple` et un seuil de **0,8** : en dessous, une personne n'est pas rendue.

Le moteur NLP `transformers` de Presidio n'est pas utilisé : il impose un modèle
Hugging Face à chaque langue. Désactiver le `SpacyRecognizer` en français par
`enabled: false` n'est pas possible non plus : cela le retire aussi de l'anglais.

### Pourquoi une sous-classe Python

`MappedLabelsNerRecognizer` est le `HuggingFaceNerRecognizer` de Presidio, à deux
différences près :

1. **Seules les étiquettes de `label_mapping` sortent.** Presidio rend une
   étiquette non mappée sous le nom que lui donne le modèle ; `LOC`, `ORG` et
   `MISC` deviendraient des types d'entité hors de la liste déclarée.
2. **Tout le texte est lu par le modèle.** Le modèle lit 512 tokens à la fois et
   ce qui dépasse est tronqué. Le découpeur de Presidio ne coupe que sur une
   espace ou un saut de ligne, aussi loin soient-ils : un long texte sans blanc
   part d'un seul bloc. Ici :
   - un morceau fait au plus `chunk_size` caractères (400). Il se termine sur le
     dernier blanc trouvé dans cette limite, sinon à 400 caractères ; le suivant
     reprend au premier mot du recouvrement (`chunk_overlap`, 40 caractères),
     sinon 40 caractères avant la coupe ;
   - un morceau dont les tokens dépassent encore la fenêtre (certains caractères
     valent plusieurs tokens : « ½ », « ﷺ ») est redécoupé en deux, autant de
     fois qu'il le faut.

La classe doit aussi s'inscrire dans `CONFIG_MODEL_MAP` : Presidio valide le
fichier de reconnaisseurs par nom de classe, et sans cela les champs du
reconnaisseur Hugging Face (`model_name`, `label_mapping`, `threshold`…) sont
ignorés pour une sous-classe. Enfin, le fichier de reconnaisseurs ne peut nommer
qu'une classe déjà importée : c'est le rôle de `analyzer_app.py`, qui remplace
`app` dans la commande de démarrage de l'image de base.

## Reconnaisseurs français ajoutés

`FR_NIR`, `FR_PHONE_NUMBER`, `FR_POSTAL_CODE`, `FR_SIRET`.

**Limite importante :** un reconnaisseur Presidio « custom » ne fait que du
motif — il ne vérifie **aucune clé de contrôle**. Un NIR au bon format mais à la
clé fausse est accepté ici. C'est pourquoi la validation par clé (NIR mod 97,
IBAN mod 97, Luhn) vit côté Go dans `backend/pkg/piidetect/validate.go` et
s'exécute **avant** Presidio : elle distingue « ressemble à » de « est un ».

## Construire et lancer

Le service est déclaré dans `compose.dev.yml` et se construit automatiquement :

```bash
docker compose -f compose.dev.yml up -d presidio-analyzer
```

La langue par défaut du backend doit suivre : `PRESIDIO_DEFAULT_LANGUAGE=fr`.

**La construction a besoin du réseau** : elle télécharge le modèle spaCy
(github.com), PyTorch (download.pytorch.org), transformers (PyPI) et le modèle
CamemBERT (huggingface.co). Elle vérifie ensuite, réseau interdit, que le modèle
embarqué se charge et reconnaît une personne.

**En fonctionnement l'image ne joint jamais le réseau** : `HF_HUB_OFFLINE=1` et
`TRANSFORMERS_OFFLINE=1` y sont fixés, et le modèle est chargé depuis un chemin
local. Un conteneur lancé avec `--network none` répond normalement.

Le modèle est chargé au démarrage : `/health` ne répond qu'une fois l'image prête
à analyser. L'image tourne sous l'utilisateur non privilégié de l'image de base
(`presidio`, uid 1001).

## Versions épinglées

Tout est fixé dans le `Dockerfile` :

| Élément | Valeur | Où |
|---|---|---|
| Image de base | `mcr.microsoft.com/presidio-analyzer:2.2.362`, par étiquette **et** par empreinte | `FROM` |
| Modèle spaCy français | `fr_core_news_md-3.8.0` | `ARG SPACY_FR_MODEL` |
| PyTorch (CPU seul) | `2.14.1+cpu` | `ARG TORCH_VERSION` |
| transformers | `5.19.0` | `ARG TRANSFORMERS_VERSION` |
| sentencepiece, protobuf | `0.2.2`, `7.36.2` | `ARG SENTENCEPIECE_VERSION`, `ARG PROTOBUF_VERSION` |
| Modèle CamemBERT | `Jean-Baptiste/camembert-ner`, commit `ef35fe7767c1dad71f5c853838cdd80d0b3441ed`, poids au seul format safetensors | `ARG CAMEMBERT_REVISION` |

Les dépendances que ces paquets entraînent (`huggingface_hub`, `tokenizers`,
`safetensors`…) ne sont pas épinglées une à une.

Pour faire évoluer une version :

- **image de base** : changer l'étiquette et l'empreinte ensemble
  (`docker buildx imagetools inspect mcr.microsoft.com/presidio-analyzer:<étiquette>`
  donne l'empreinte). `mapped_ner_recognizer.py` s'appuie sur le
  `HuggingFaceNerRecognizer`, le découpeur et la validation du registre de cette
  version de Presidio : les relire à chaque montée ;
- **PyTorch, transformers, modèle** : changer l'`ARG`. La construction échoue si
  le tokenizer rapide n'est plus disponible ou si la fenêtre du modèle n'est plus
  de 512 tokens ;
- dans tous les cas, relancer les tests de l'image (ci-dessous).

## Réglages

- **`OMP_NUM_THREADS`** (2 par défaut dans l'image) : PyTorch dimensionne son
  pool de threads sur les cœurs de l'hôte, pas sur le quota CPU du conteneur.
  Sous un quota, les threads en trop se disputent le temps alloué. Donner à
  `OMP_NUM_THREADS` le nombre de CPU réellement alloués au conteneur.
- **`WORKERS`** (1 par défaut, hérité de l'image de base) : nombre de processus
  gunicorn. Chaque processus charge ses propres modèles et son propre pool de
  threads : la mémoire ci-dessous se compte par processus, et
  `WORKERS × OMP_NUM_THREADS` ne devrait pas dépasser les CPU alloués.

## Coûts mesurés

Une observation par cas, sur un hôte à 16 cœurs, pour un texte français de
2 000 caractères (un seul `POST /analyze`, après une première requête courte) :

| | Cette image | Avant CamemBERT |
|---|---|---|
| Taille de l'image, décompressée | 3,9 Go | 1,69 Go |
| Taille de l'image, compressée | 1,31 Go | 614 Mo |
| Mémoire après démarrage | 1,31 Gio | 1,12 Gio |
| Démarrage jusqu'à `/health` | 9 à 10 s | 6,5 s |
| 2 000 caractères, sans quota | 0,55 s | 0,07 s |
| 2 000 caractères, `--cpus 2` | 0,55 s | — |
| 2 000 caractères, `--cpus 2` et `OMP_NUM_THREADS` non fixé | 1,85 s | — |

Un texte de 5 000 caractères sans aucun blanc prend environ 3 s sans quota.

## Tests

`internal/integration-tests/presidio` construit l'image et l'interroge en HTTP.
Comme la construction télécharge PyTorch et le modèle, ces tests ne tournent que
sur demande :

```bash
PRESIDIO_IMAGE_TESTS=1 go test ./internal/integration-tests/presidio/... -count=1 -timeout 30m
```

L'image construite reste sur la machine sous le nom
`husonym-presidio-analyzer-test:latest` (3,9 Go), pour que le lancement suivant
réutilise ses couches.

## Licences de ce que l'image ajoute

| Composant | Licence |
|---|---|
| Presidio Analyzer (image de base) | MIT |
| spaCy | MIT |
| `fr_core_news_md` | LGPL-LR |
| PyTorch | Apache-2.0 (et BSD, MIT, BSL-1.0 pour ses composants) |
| transformers, tokenizers, safetensors, huggingface_hub, sentencepiece | Apache-2.0 |
| protobuf | BSD-3-Clause |
| `Jean-Baptiste/camembert-ner` | MIT (fiche du modèle, copiée dans l'image) |
