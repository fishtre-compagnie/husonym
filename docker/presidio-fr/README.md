# Presidio Analyzer francisé

Image dérivée de `mcr.microsoft.com/presidio-analyzer`, ajoutant le français : un
catalogue de reconnaisseurs français, le modèle spaCy `fr_core_news_md`, et un
modèle DistilCamemBERT quantifié, exécuté par ONNX Runtime, pour reconnaître les
personnes.

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

Mesuré au banc `scripts/testdata/bench-presidio.py` avec le moteur français
précédent (spaCy pour les personnes), le passage au catalogue français faisait
progresser le F1 de **0,72 à 0,80** (rappel 71 % → 77 %, précision 73 % → 83 %)
et supprimait le seul faux positif. Ces chiffres n'ont pas été refaits avec le
moteur actuel ; la mesure faite sur cette image est plus bas (« Reconnaissance
mesurée sur des textes métier »).

## Contenu

| Fichier | Rôle |
|---|---|
| `Dockerfile` | en deux étapes : la première télécharge le modèle des personnes, l'exporte en ONNX et le quantifie ; la seconde ajoute `fr_core_news_md`, ONNX Runtime, tokenizers et le modèle exporté, copie la configuration et fixe les variables qui la désignent |
| `export_model.py` | l'export et la quantification du modèle, exécutés à la première étape ; n'est pas dans l'image |
| `nlp.yaml` | moteur NLP anglais + français (spaCy), avec le mapping d'entités spaCy fr (PER/LOC…) |
| `recognizers.yaml` | catalogue en + fr : reconnaisseurs français absents de Presidio, et reconnaisseur des personnes en français |
| `analyzer.yaml` | langues du moteur d'analyse |
| `onnx_ner_recognizer.py` | le reconnaisseur des personnes en français |
| `analyzer_app.py` | point d'entrée du serveur : importe le reconnaisseur, puis lance l'application de Presidio |
| `recognizer_check.py` | contrôles du reconnaisseur sans le modèle, exécutés à la construction |
| `server_check.py` | contrôle du serveur avec le vrai modèle, exécuté à la construction : une phrase française, une personne rendue par le reconnaisseur de l'image |

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

## Personnes en français : DistilCamemBERT

spaCy reste le moteur NLP des deux langues. En anglais rien ne change : les
personnes viennent du `SpacyRecognizer`. En français :

- le `SpacyRecognizer` ne rend que `LOCATION` : `fr_core_news_md` n'a que les
  étiquettes `LOC`, `MISC`, `ORG` et `PER`, et `PERSON` ne lui est plus demandé.
  `DATE_TIME` et `NRP` restent dans sa déclaration (`recognizers.yaml`) parce
  que la liste de `/supportedentities` en est déduite, mais spaCy ne les rend
  pas en français, et c'était déjà le cas avec l'image précédente : une date en
  chiffres (« 03/03/1984 ») vient du `DateRecognizer`, qui travaille par
  motifs ; une date écrite en lettres (« née le 3 mars 1984 ») n'est pas
  désignée ; `NRP` est déclaré et n'est jamais rendu ;
- les personnes viennent de `OnnxNerRecognizer`, qui interroge le modèle
  [`cmarkea/distilcamembert-base-ner`](https://huggingface.co/cmarkea/distilcamembert-base-ner)
  embarqué dans l'image (`/app/models/distilcamembert-ner`), avec un seuil de
  **0,6** : en dessous, une personne n'est pas rendue.

Le moteur NLP `transformers` de Presidio n'est pas utilisé : il impose un modèle
Hugging Face à chaque langue. Désactiver le `SpacyRecognizer` en français par
`enabled: false` n'est pas possible non plus : cela le retire aussi de l'anglais.

### Le modèle, exporté et quantifié à la construction

La première étape du `Dockerfile` télécharge le modèle à une révision épinglée,
vérifie l'empreinte de ses poids, puis lance `export_model.py`, qui écrit ce que
le reconnaisseur charge :

- `model.onnx` : le modèle exporté au format ONNX, ses poids quantifiés sur
  8 bits (68 Mo, contre 270 Mo pour les poids d'origine) ;
- `tokenizer.json` : le tokenizer, lisible sans transformers ;
- `ner.json` : d'où vient le modèle, ses étiquettes, et le nombre de tokens qu'il
  lit à la fois (512).

PyTorch et transformers ne servent qu'à cette étape et n'entrent pas dans
l'image, qui exécute le modèle avec ONNX Runtime.

La quantification est faite **par canal** (une échelle par canal de sortie de
chaque poids) et sur une **plage réduite**. Avec une seule échelle par poids, le
réglage par défaut de l'outil, ce modèle ne trouve plus aucun nom. La plage
réduite évite que les produits sur 8 bits saturent sur les processeurs sans
instructions VNNI : le résultat ne dépend pas du processeur. Avant de garder
l'export, le script compare les étiquettes du modèle quantifié à celles du
modèle d'origine sur quelques phrases, token par token, et arrête la
construction si elles diffèrent.

Ce que la quantification coûte, mesuré hors de l'image sur les mêmes textes : rien
sur du texte métier ordinaire, mais des noms en moins dans du texte presque sans
espaces (voir « Texte pauvre en espaces », plus bas).

Le seuil de 0,6 a été choisi sur le premier jeu de mesure (`free-text-fr.json`)
et vérifié sur un second, écrit sans exécuter de modèle (`free-text-fr-holdout.json`).

### Ce que fait le reconnaisseur

`OnnxNerRecognizer` découpe le texte, donne chaque morceau au modèle, regroupe
les tokens en entités comme la stratégie `simple` de transformers (des tokens
consécutifs de même étiquette font une entité, du début du premier à la fin du
dernier, avec la moyenne de leurs scores), et garantit quatre choses :

1. **Seules les étiquettes de `label_mapping` sortent.** Le modèle étiquette
   aussi `LOC`, `ORG` et `MISC`, qui seraient des types d'entité hors de la liste
   déclarée.
2. **Aucun morceau donné au modèle ne dépasse sa fenêtre.** Le modèle lit
   512 tokens à la fois. Le découpeur de Presidio ne coupe que sur une espace ou
   un saut de ligne, aussi loin soient-ils : un long texte sans blanc partirait
   d'un seul bloc. Ici :
   - un morceau fait au plus `chunk_size` caractères (400). Il se termine sur le
     dernier blanc trouvé dans cette limite, sinon à 400 caractères ;
   - le suivant commence `chunk_overlap` caractères (40) avant cette fin, ou sur
     le début de mot le plus proche avant ce point, cherché sur 40 caractères de
     plus. Deux morceaux consécutifs partagent donc toujours au moins
     40 caractères (80 au plus) : une portion de texte de 40 caractères ou moins
     se trouve en entier dans l'un des deux. Cela vaut pour ce que les morceaux
     contiennent, pas pour ce que le modèle y reconnaît (voir les limites plus
     bas) ;
   - un morceau dont les tokens dépassent encore la fenêtre (certains caractères
     valent plusieurs tokens : « ½ », « ﷺ ») est redécoupé en deux, autant de
     fois qu'il le faut. Rien n'est tronqué.
3. **Les trouvailles de deux morceaux qui se chevauchent sont réunies.** Deux
   morceaux partagent une portion du texte et chacun y lit un nom avec son
   propre contexte : l'un peut le rendre entier, l'autre n'en rendre qu'une
   partie (« Corentin Le »), parfois avec un meilleur score. Aucune n'est
   choisie : des trouvailles du même type qui se chevauchent dans le texte,
   directement ou par l'intermédiaire d'autres, et qui ne viennent pas toutes du
   même morceau, sont remplacées par une seule, du plus petit début à la plus
   grande fin, avec le score et l'explication de la mieux notée. Des trouvailles
   qui se chevauchent et viennent toutes du même morceau sont rendues telles
   quelles. Aucun caractère désigné par un morceau n'est perdu, aucun n'est
   ajouté.

   Ce que cela implique pour l'appelant : une trouvaille `PERSON` désigne un
   passage, pas forcément une seule personne. Deux personnes nommées l'une à
   la suite de l'autre de part et d'autre d'une coupe peuvent revenir en une
   seule trouvaille si les lectures des deux morceaux se chevauchent.
4. **Une inférence qui échoue fait échouer la requête.** Le serveur répond par
   une erreur au lieu d'une liste incomplète.

Le reconnaisseur est une classe de cette image, pas une sous-classe du
reconnaisseur Hugging Face de Presidio. Deux détails de Presidio le contraignent
pourtant :

- le fichier de reconnaisseurs est validé par nom de classe, et le registre ne
  transmet que les champs des modèles de configuration que Presidio déclare
  lui-même. La classe s'inscrit donc dans `CONFIG_MODEL_MAP` avec le modèle de
  configuration du reconnaisseur Hugging Face, et en prend cinq champs :
  `model_name` (ici le dossier du modèle exporté), `label_mapping`, `threshold`,
  `chunk_size`, `chunk_overlap`. Les autres champs de ce modèle
  (`aggregation_strategy`, `device`…) sont refusés au démarrage s'ils sont
  renseignés ; un champ inconnu est refusé par la validation ;
- le fichier ne peut nommer qu'une classe déjà importée : c'est le rôle de
  `analyzer_app.py`, qui remplace `app` dans la commande de démarrage de l'image
  de base.

Ces points, et le découpeur, s'appuient sur des noms internes de Presidio. Le
module les vérifie à l'import : s'il en manque un, la construction échoue et
l'image ne démarre pas.

`recognizer_check.py` contrôle le découpage (sur des textes aléatoires avec et
sans blancs, et sur des noms de plusieurs mots placés autour des coupes), la
réunion des trouvailles, le regroupement des tokens, le filtre des étiquettes,
les réglages refusés et l'erreur d'inférence, avec un faux modèle. La
construction l'exécute, et il reste dans l'image : il se relance sur une image
construite, sans réseau ni modèle chargé :

```bash
docker run --rm --network none --entrypoint python <image> -B /app/recognizer_check.py
```

Pour le lancer pendant qu'on modifie `onnx_ner_recognizer.py`, sans reconstruire
toute l'image, monter le dossier sur une image déjà construite (l'image de base
n'a ni ONNX Runtime ni tokenizers) :

```bash
docker run --rm --network none -e PYTHONPATH=/app \
  -v "$PWD/docker/presidio-fr":/src:ro -w /src --entrypoint python \
  <image> -B recognizer_check.py
```

### Texte pauvre en espaces

Dans un texte presque sans espaces (JSON compact, liste de références collées),
le modèle reconnaît moins bien les noms, où qu'ils soient ; une coupe qui tombe
dans un nom de plusieurs mots y ajoute. La réunion des trouvailles ne corrige
que ce que les morceaux rendent. Mesuré sur cette image
(`Test_Analyzer_French_NamesOfSeveralWords_InTextWithFewSpaces`), trois noms
placés à 60 positions autour d'une fin de morceau, dans deux textes dont les
seules espaces sont celles du nom :

| Nom | Liste de références | JSON compact |
|---|---|---|
| « Corentin Le Guével » | entier à 52 positions sur 60 | 38 sur 60 |
| « Mathilde Rousseau de Kerbrat » | 60 sur 60 | 57 sur 60 |
| « Anne-Sophie Marchand » | 37 sur 60 | 56 sur 60 |

Aux autres positions le nom revient en partie (« Corentin Le », « entin Le
Guével »), avec un caractère de trop devant lui (un guillemet), ou pas du tout.
Dans tous les cas une trouvaille ne désigne que des caractères du nom ou des
deux qui le précèdent.

Hors de l'image, sur les mêmes textes, le modèle non quantifié rend ces trois
noms entiers aux 60 positions en JSON compact, mais « Anne-Sophie Marchand » à
45 positions sur 60 seulement dans la liste de références : la quantification
explique une part de ces manques, le modèle lui-même le reste.

**Un nom que le modèle ne voit pas.** « Corentin de La Brosse » dans une valeur
JSON (`{"nom":"Corentin de La Brosse"}`) n'est rendu à aucune position, quantifié
ou non, alors que « Mathilde Rousseau de Kerbrat » l'est. Deux cas : trop peu
pour fixer une règle sur les noms à particule.

Dans de la prose, les mêmes noms placés autour d'une coupe reviennent entiers à
toutes les positions essayées (`Test_Analyzer_French_Names_InProse_AroundAChunkBoundary`).

## Reconnaisseurs français ajoutés

`FR_NIR`, `FR_PHONE_NUMBER`, `FR_POSTAL_CODE`, `FR_SIRET`.

**Limite importante :** un reconnaisseur Presidio « custom » ne fait que du
motif — il ne vérifie **aucune clé de contrôle**. Un NIR au bon format mais à la
clé fausse est accepté ici. C'est pourquoi la validation par clé (NIR mod 97,
IBAN mod 97, Luhn) vit côté Go dans `backend/pkg/piidetect/validate.go` et
s'exécute **avant** Presidio : elle distingue « ressemble à » de « est un ».

## Construire et lancer

Le service est déclaré dans `compose.dev.yml` :

```bash
docker compose -f compose.dev.yml up -d --build presidio-analyzer
```

Le service porte `pull_policy: build` : `docker compose up` construit l'image à
chaque lancement, avec ou sans `--build` (les couches inchangées viennent du
cache). Sans cela, une image locale construite quand la configuration était
encore montée par le fichier compose serait relancée telle quelle, sans
configuration française. **Le symptôme** : le conteneur démarre, `/health`
répond, et chaque appel en français reçoit une erreur 500 « No matching
recognizers were found ». Le même symptôme se voit avec un fichier compose qui
n'a pas `pull_policy: build`, ou avec une image lancée par `docker run` sans
avoir été reconstruite : reconstruire l'image le fait disparaître.

La langue par défaut du backend doit suivre : `PRESIDIO_DEFAULT_LANGUAGE=fr`.

**La construction a besoin du réseau.** Au-delà du registre de l'image de base,
les hôtes observés à une construction :

| Ce qui est téléchargé | Hôtes |
|---|---|
| modèle spaCy | `github.com`, qui redirige vers `release-assets.githubusercontent.com` |
| PyTorch (première étape) | `download.pytorch.org` (index), `download-r2.pytorch.org` (fichiers) |
| ONNX Runtime, tokenizers, transformers et les autres paquets | PyPI : `pypi.org` (index), `files.pythonhosted.org` (fichiers) |
| modèle des personnes | `huggingface.co`, et pour les poids les hôtes de fichiers de Hugging Face sous `hf.co` : `cas-server.xethub.hf.co` avec le client `hf_xet` que la première étape installe, `us.aws.cdn.hf.co` par la redirection de `huggingface.co` sans lui |

Les hôtes de fichiers sont ceux d'un jour donné : ils peuvent changer sans que
ce dossier change.

La première étape installe PyTorch (CPU seul) et transformers : elle télécharge
et occupe plusieurs gigaoctets dans le cache de construction, qui n'entrent pas
dans l'image.

La construction contrôle ensuite ce qu'elle a téléchargé et assemblé :

- les poids (`model.safetensors`) sont comparés à leur empreinte SHA-256
  (`ARG NER_WEIGHTS_SHA256`), celle que Hugging Face publie pour ce fichier à la
  révision épinglée ; s'ils diffèrent, la construction s'arrête ;
- `export_model.py` compare le modèle quantifié au modèle d'origine avant de le
  garder (voir plus haut) ;
- `recognizer_check.py` contrôle le reconnaisseur sans le modèle ;
- `server_check.py` crée l'application comme gunicorn la crée
  (`analyzer_app.create_app()` : le registre, les trois fichiers de
  configuration, le vrai modèle), sous l'utilisateur de l'image, lui envoie une
  phrase française et exige une trouvaille `PERSON` de l'`OnnxNerRecognizer`,
  d'un score entre 0,6 et 1, sans type d'entité hors des seize déclarés. Un nom
  de Presidio, d'ONNX Runtime ou de tokenizers qui a changé arrête la
  construction ici.

**En fonctionnement l'image ne joint jamais le réseau** : le modèle et son
tokenizer sont lus dans un dossier de l'image, et rien n'y est téléchargé. Un
conteneur lancé avec `--network none` répond normalement.

Le modèle est chargé au démarrage : `/health` ne répond qu'une fois l'image prête
à analyser. Ce délai dépend du CPU alloué — observé, une fois par cas : 8 s sans
quota, 10 s avec `--cpus 1`, 31 s et 37 s (deux observations) avec `--cpus 0.5`.
Le contrôle de santé hérité de l'image de base (un essai toutes les 30 s, 3 s
d'attente, 30 s de période de démarrage, 3 essais) laisse le conteneur
« starting » pendant ce délai ; un contrôle plus strict (`healthcheck` de
compose, sondes de Kubernetes) doit laisser au moins ce délai avant de déclarer
le conteneur en échec. Le même délai s'ajoute à la première requête servie par
un worker que gunicorn vient de remplacer (voir `WORKER_TIMEOUT`, plus bas).
L'image tourne sous l'utilisateur non privilégié de l'image de base (`presidio`,
uid 1001).

**La commande de démarrage porte `--no-control-socket`.** gunicorn 25.1.0, celui
de l'image de base, ouvre un socket de contrôle dans un thread juste avant de
créer son worker par `fork`. Si le `fork` tombe pendant que ce thread écrit sa
ligne de journal (« Control socket listening at … »), le worker hérite d'un
verrou que personne ne relâchera : il reste bloqué sur sa première ligne de
journal, n'importe jamais l'application, et `/health` ne répond pas. gunicorn
ne le remplace pas de lui-même ; un `SIGHUP` au processus maître en crée un
nouveau. Ce défaut de gunicorn est décrit dans
[l'issue n° 3529](https://github.com/benoitc/gunicorn/issues/3529) et corrigé par
[la pull request n° 3520](https://github.com/benoitc/gunicorn/pull/3520)
(« prevent fork deadlock… »), qui ferme l'issue n° 3509 ; le correctif est dans
le code de gunicorn 25.2.0. Mesuré sur une construction antérieure de cette
image, avec la même image de base et la même commande, un démarrage à la fois
sur un CPU : 5 démarrages bloqués sur 53 sans l'option, 0 sur 60 avec. Le
blocage a aussi été observé sur l'image précédente de ce dossier lancée sous
l'utilisateur 1001 (1 démarrage sur 30), et pas lancée en root (0 sur 60) ; on
ne sait pas pourquoi il ne se montre pas en root. Ce socket ne sert pas ici (il
pilote gunicorn par la commande `gunicornc`). L'option est à retirer quand
l'image de base embarquera gunicorn 25.2.0 ou plus.

## Versions épinglées

Ce que le `Dockerfile` installe nommément y est fixé :

| Élément | Valeur | Où |
|---|---|---|
| Image de base, des deux étapes | `mcr.microsoft.com/presidio-analyzer:2.2.362`, par étiquette **et** par empreinte | `ARG BASE_IMAGE` |
| Modèle spaCy français | `fr_core_news_md-3.8.0` | `ARG SPACY_FR_MODEL` |
| ONNX Runtime, des deux étapes | `1.30.0` | `ARG ONNXRUNTIME_VERSION` |
| tokenizers, des deux étapes | `0.23.2` | `ARG TOKENIZERS_VERSION` |
| PyTorch (CPU seul), première étape | `2.14.1+cpu` | `ARG TORCH_VERSION` |
| transformers, première étape | `5.19.0` | `ARG TRANSFORMERS_VERSION` |
| onnx, sentencepiece, protobuf, première étape | `1.23.2`, `0.2.2`, `7.36.2` | `ARG ONNX_VERSION`, `ARG SENTENCEPIECE_VERSION`, `ARG PROTOBUF_VERSION` |
| Modèle des personnes | `cmarkea/distilcamembert-base-ner`, commit `e539d952f70088b450c300c28ba455da87f2dc4b`, poids au seul format safetensors | `ARG NER_REPO`, `ARG NER_REVISION` |
| Poids du modèle | SHA-256 de `model.safetensors`, `3e05cfc8…a97c`, vérifié à la construction | `ARG NER_WEIGHTS_SHA256` |

**Ce qui ne l'est pas** : les dépendances que ces paquets entraînent prennent la
version que pip résout le jour de la construction. Dans l'image, ce sont
`flatbuffers` et `protobuf` (pour ONNX Runtime), `huggingface_hub`, `hf_xet` et
`fsspec` (pour tokenizers) ; un paquet de l'image de base est aussi monté
(`click`). Deux constructions à des dates différentes peuvent donc différer sur
ces paquets, et sur ceux de la première étape. Le modèle exporté n'est pas
comparé à une empreinte : il est recalculé à chaque construction sans cache.

Pour faire évoluer une version :

- **image de base** : changer l'étiquette et l'empreinte ensemble
  (`docker buildx imagetools inspect mcr.microsoft.com/presidio-analyzer:<étiquette>`
  donne l'empreinte). `onnx_ner_recognizer.py` s'appuie sur le découpeur et la
  validation du registre de cette version de Presidio : les relire à chaque
  montée ;
- **ONNX Runtime, tokenizers** : changer l'`ARG` en tête du `Dockerfile`, qui vaut
  pour les deux étapes — le modèle est quantifié et relu par la même version ;
- **PyTorch, transformers, modèle** : changer l'`ARG`. La construction échoue si
  le tokenizer rapide n'est plus disponible, si la fenêtre du modèle n'est plus
  de 512 tokens, ou si le modèle quantifié s'écarte du modèle d'origine. Une
  autre révision du modèle demande aussi l'empreinte de ses poids
  (`ARG NER_WEIGHTS_SHA256`) : celle que la page du fichier `model.safetensors`
  affiche sur huggingface.co pour cette révision. Un autre modèle demande de
  refaire la mesure et de revoir le seuil (`threshold` dans `recognizers.yaml`) ;
- dans tous les cas, relancer les tests de l'image (ci-dessous).

## Réglages

- **`OMP_NUM_THREADS`** (2 par défaut dans l'image) : le nombre de threads d'une
  inférence. ONNX Runtime dimensionne son pool de threads sur les cœurs de
  l'hôte, pas sur le quota CPU du conteneur ; le reconnaisseur de l'image lit
  cette variable pour le fixer. Sous un quota, les threads en trop se disputent
  le temps alloué : donner à `OMP_NUM_THREADS` le nombre de CPU réellement
  alloués au conteneur. À 0, ONNX Runtime reprend son réglage par défaut.
- **`WORKERS`** (1 par défaut, hérité de l'image de base) : nombre de processus
  gunicorn. Chaque processus charge ses propres modèles et son propre pool de
  threads : la mémoire ci-dessous se compte par processus, et
  `WORKERS × OMP_NUM_THREADS` ne devrait pas dépasser les CPU alloués.
- **`WORKER_TIMEOUT`** (120 par défaut, propre à cette image) : durée, en
  secondes, au bout de laquelle gunicorn tue un worker qui n'a pas fini sa
  requête ; c'est son option `--timeout`, qui vaut 30 sans ce réglage. Un worker
  tué répond par une erreur 500 (une page HTML, pas du JSON), le journal porte
  `[CRITICAL] WORKER TIMEOUT`, et son remplaçant recharge les modèles avant de
  servir la requête suivante. La valeur par défaut est au-dessus des 60 s
  qu'attend le client de Husonym (`backend/pkg/presidio`) : c'est la limite du
  client qu'un appelant rencontre, pas celle du worker. Avec un autre client,
  régler `WORKER_TIMEOUT` au-dessus de sa limite. Le worker de gunicorn ne
  traite qu'une requête à la fois et ne sait pas que l'appelant a renoncé : il
  finit le texte abandonné, ou est tué à `WORKER_TIMEOUT`, et les requêtes
  suivantes attendent derrière lui.

## Coûts mesurés

Une observation par chiffre, sur un hôte à 16 cœurs : un seul `POST /analyze` en
français après une première requête courte, `OMP_NUM_THREADS=2` sauf mention.
Ce ne sont pas des garanties : le temps dépend de la machine. La colonne « image
précédente » est celle de ce dossier avant le modèle des personnes (spaCy pour
tout), mesurée à un autre moment.

| | Cette image | Image précédente |
|---|---|---|
| Taille de l'image, décompressée | 1,95 Go | 1,69 Go |
| Taille de l'image, compressée | 0,70 Go | 0,61 Go |
| Mémoire après démarrage | 1,16 Gio | 1,12 Gio |
| Démarrage jusqu'à `/health`, sans quota | 8 s | 6,5 s |
| Démarrage jusqu'à `/health`, `--cpus 1` | 10 s | — |
| Démarrage jusqu'à `/health`, `--cpus 0.5` | 31 s, 37 s | — |

**Temps d'analyse d'un texte.** Il croît avec la longueur du texte, et dépend de
deux choses : le CPU alloué, et la part de blancs du texte. Un texte pauvre en
blancs (JSON compact, liste de références, journal sans espaces) a coûté ici
environ trois fois ce que coûte une prose de même longueur.

| Texte | CPU | Temps | Par 1 000 caractères |
|---|---|---|---|
| prose, 2 000 caractères | sans quota | 0,12 s | 0,06 s |
| prose, 20 000 caractères | sans quota | 1,3 s | 0,06 s |
| prose, 200 000 caractères | sans quota | 14,4 s | 0,07 s |
| JSON compact, 5 000 caractères | sans quota | 1,1 s | 0,2 s |
| JSON compact, 50 000 caractères | sans quota | 10,6 s | 0,2 s |
| prose, 2 000 caractères | `--cpus 2` | 0,13 s | 0,06 s |
| prose, 2 000 caractères | `--cpus 2`, `OMP_NUM_THREADS=0` | 0,63 s | 0,3 s |
| prose, 10 000 caractères | `--cpus 1` | 1,6 s | 0,16 s |
| JSON compact, 5 000 caractères | `--cpus 1` | 2,4 s | 0,5 s |
| prose, 10 000 caractères | `--cpus 1`, `OMP_NUM_THREADS=1` | 1,5 s | 0,15 s |
| JSON compact, 5 000 caractères | `--cpus 1`, `OMP_NUM_THREADS=1` | 1,6 s | 0,3 s |
| prose, 2 000 caractères | `--cpus 0.5` | 0,55 s | 0,3 s |

**Par rapport à l'image précédente**, dont les temps ont été relevés sur d'autres
textes de même nature : une prose de 2 000 caractères y prenait 0,07 s, une de
20 000 caractères 0,3 s, un JSON compact de 5 000 caractères 0,07 s. Une valeur
courte coûte donc environ deux fois plus, une longue prose quatre fois plus, et
un texte pauvre en blancs une quinzaine de fois plus.

**Un texte dont l'analyse dépasse la limite du client échoue.** Le client de
Husonym attend 60 s. Passé ce délai, la valeur échoue : le transformer
`Transform PII Text` arrête le run sur elle, et le scan de contenu signale la
colonne comme non analysée, ainsi que celles qu'il lui restait à lire. Ordres de
grandeur déduits des débits ci-dessus, non mesurés : 60 s correspondent à environ
800 000 caractères de prose sans quota, 280 000 caractères de JSON compact sans
quota, 400 000 caractères de prose à un CPU, 120 000 caractères de JSON compact à
un CPU. Des requêtes simultanées se partagent le même CPU et attendent l'une
derrière l'autre : ces longueurs sont des plafonds, pas des marges.

## Reconnaissance mesurée sur des textes métier

La mesure est `internal/integration-tests/presidio/measure_integration_test.go`,
sur des jeux inventés de `testdata/` : 12 colonnes de 50 valeurs chacun (6
colonnes qui nomment des personnes, 6 qui n'en nomment pas : noms de sociétés, de
produits, villes, codes). Chaque valeur est tronquée à ses 200 premiers
caractères et envoyée seule, avec `score_threshold` à 0,35. Un passage désigné
est juste quand il chevauche un nom annoté de sa valeur ; un nom est trouvé quand
un passage `PERSON` le chevauche. Un titre (« Mme », « M. », « Mr ») n'est pas
compté dans le nom annoté. Ce sont des chiffres de ces jeux, une mesure par cas :
pas une garantie sur d'autres textes.

Il y a deux jeux français. Le seuil du reconnaisseur a été choisi sur le premier
(`free-text-fr.json`, 32 noms). Le second (`free-text-fr-holdout.json`, 109 noms)
a été écrit sans exécuter de modèle, avec d'autres métiers et des formes de noms
plus variées (nom en capitales, prénom seul, particule, initiale, nom tapé en
minuscules) : c'est lui qui dit ce que vaut l'image sur un texte pour lequel elle
n'a pas été réglée. Le jeu anglais (`free-text-en.json`) compte 32 noms.

| | Français, premier jeu | Français, second jeu | Anglais |
|---|---|---|---|
| Passages `PERSON` rendus | 31 | 106 | 48 |
| Passages justes, précision | 31, soit 1,00 | 102, soit 0,96 | 30, soit 0,63 |
| Noms trouvés, rappel | 31 sur 32, soit 0,97 | 102 sur 109, soit 0,94 | 30 sur 32, soit 0,94 |
| Valeurs désignées dans les colonnes sans personne | 0 sur 300 | 3 sur 300 | 16 sur 300 |
| Colonnes sans personne avec 2 valeurs désignées ou plus | 0 sur 6 | 0 sur 6 | 3 sur 6 |

Sur le premier jeu et au même seuil de requête, l'image précédente désignait
32 valeurs des colonnes sans personne, et trouvait les 32 noms. L'anglais n'a pas
changé : ses personnes viennent toujours du modèle spaCy de l'image de base.

**Ce que la mesure a manqué ou désigné à tort, à lire comme des limites :**

- français, des prénoms seuls : 6 des 8 noms manqués sont un prénom sans nom de
  famille (« vu avec Bérénice », « Théo et Maud »). Les deux autres sont
  « Tomasz Wrobel » et « Marine Lefort » ;
- français, quatre passages à tort sur 137 : un mot en tête de valeur
  (« Sinistre »), un nom de produit (« Liftéa »), un nom de salle (« Mistral »),
  un nom de lieu (« Bellevue ») ;
- français, un titre (« Mme », « M. ») placé devant le nom est parfois dans le
  passage désigné et parfois non : qui réécrit le passage peut laisser le titre
  en place ;
- français, texte presque sans espaces : voir « Texte pauvre en espaces » ;
- anglais, deux noms manqués (« Grace Barnes » dans une remarque de commande,
  « Draper » après « Mr ») ; 18 passages à tort : noms de sociétés, un nom
  commun en début de phrase, numéros de commande (« SO-362398 »), un mot d'état
  (« Voicemail ») ;
- `LOCATION` vient toujours de spaCy en français : le modèle des personnes n'y
  change rien. spaCy ne rend ni `DATE_TIME` ni `NRP` en français (voir plus
  haut). Dans les colonnes françaises sans personne du premier jeu, des valeurs
  portent encore des types sensibles autres que `PERSON` (sur 300 valeurs :
  73 `LOCATION`, 30 `PHONE_NUMBER`, 9 `FR_PHONE_NUMBER`, 8 `FR_SIRET`) ; dans
  celles du second, 68 `LOCATION`.

## Tests

`internal/integration-tests/presidio` construit l'image et l'interroge en HTTP.
Comme la construction télécharge PyTorch et le modèle, ces tests ne tournent que
sur demande :

```bash
PRESIDIO_IMAGE_TESTS=1 go test ./internal/integration-tests/presidio/... -count=1 -timeout 30m
```

**Ce qu'un lancement laisse sur la machine.** L'image construite reste sous le
nom `husonym-presidio-analyzer-test:latest` (1,95 Go), pour que le lancement
suivant réutilise ses couches. Un lancement fait après une modification de ce
dossier construit une nouvelle image sous ce nom et laisse la précédente sans
nom. La construction faite par les tests laisse aussi la première étape, celle
qui porte PyTorch, en image sans nom (3,7 Go observés). Pour les retirer :

```bash
docker rmi husonym-presidio-analyzer-test:latest
docker images -a --filter dangling=true   # celles restées sans nom
docker rmi <id>                           # pour chacune de celles-là
```

## Licences

Ce que cette image ajoute à l'image de base, d'après les métadonnées des paquets
installés :

| Composant | Licence |
|---|---|
| `fr_core_news_md` | LGPL-LR |
| ONNX Runtime (`onnxruntime`) | MIT |
| `tokenizers`, `huggingface_hub`, `hf_xet`, `flatbuffers` | Apache-2.0 |
| `protobuf`, `fsspec` | BSD-3-Clause |
| `click` (déjà dans l'image de base, monté ici) | BSD-3-Clause |
| `cmarkea/distilcamembert-base-ner` | MIT (fiche du modèle, copiée dans l'image avec le modèle exporté) |

PyTorch et transformers servent à exporter le modèle à la construction et ne sont
pas dans l'image. L'image de base apporte Presidio Analyzer (MIT), spaCy (MIT) et
le modèle `en_core_web_lg` (MIT), avec leurs propres dépendances.
