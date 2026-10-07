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
| `recognizer_check.py` | contrôles du reconnaisseur sans PyTorch ni modèle, exécutés à la construction |

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

`MappedLabelsNerRecognizer` est le `HuggingFaceNerRecognizer` de Presidio, à quatre
différences près :

1. **Seules les étiquettes de `label_mapping` sortent.** Presidio rend une
   étiquette non mappée sous le nom que lui donne le modèle ; `LOC`, `ORG` et
   `MISC` deviendraient des types d'entité hors de la liste déclarée.
2. **Aucun morceau donné au modèle ne dépasse sa fenêtre.** Le modèle lit
   512 tokens à la fois et ce qui dépasse est tronqué. Le découpeur de Presidio
   ne coupe que sur une espace ou un saut de ligne, aussi loin soient-ils : un
   long texte sans blanc part d'un seul bloc. Ici :
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
     fois qu'il le faut.
3. **Les trouvailles de deux morceaux qui se chevauchent sont réunies.** Deux
   morceaux partagent une portion du texte et chacun y lit un nom avec son
   propre contexte : l'un peut le rendre entier, l'autre n'en rendre qu'une
   partie (« Corentin Le »), parfois avec un meilleur score. Presidio ne garde
   alors que la trouvaille au meilleur score quand leur partie commune dépasse
   la moitié de la plus courte, et les garde toutes les deux sinon. Ici aucune
   n'est choisie : des trouvailles du même type qui se chevauchent dans le
   texte, directement ou par l'intermédiaire d'autres, et qui ne viennent pas
   toutes du même morceau, sont remplacées par une seule, du plus petit début
   à la plus grande fin, avec le score et l'explication de la mieux notée.
   Des trouvailles qui se chevauchent et viennent toutes du même morceau sont
   rendues telles quelles. Aucun caractère désigné par un morceau n'est perdu,
   aucun n'est ajouté.

   Ce que cela implique pour l'appelant : une trouvaille `PERSON` désigne un
   passage, pas forcément une seule personne. Deux personnes nommées l'une à
   la suite de l'autre de part et d'autre d'une coupe peuvent revenir en une
   seule trouvaille si les lectures des deux morceaux se chevauchent.

   **Limites, que la réunion ne corrige pas.** Elle ne réunit que ce que les
   morceaux rendent :
   - dans un long texte avec peu ou pas de blancs, quand la coupe tombe dans un
     nom de plusieurs mots, le modèle peut n'en rendre qu'une partie dans
     chacun des morceaux — ou rien — et l'image désigne alors cette partie.
     Mesuré : un nom en « de La » rendu entier à 93 positions sur 180 dans un
     texte dont les seules espaces sont celles du nom ; un nom de quatre mots
     rendu sans son dernier mot à 1 position sur 30 dans du JSON compact ;
   - un nom que le modèle ne voit dans aucun morceau ;
   - une trouvaille à laquelle il manque sa première lettre, ou qui commence
     par un tiret placé juste avant le nom : observé dans les essais de
     balayage des coupes (par exemple « -Mathilde Rousseau de Kerbrat »,
     « athilde Rousseau de Kerbrat »), sans taux établi au-delà de ces essais.
4. **Une inférence qui échoue fait échouer la requête.** Presidio rend alors
   « aucune trouvaille » pour le morceau ; ici l'erreur remonte, et le serveur
   répond par une erreur au lieu d'une liste incomplète.

La classe doit aussi s'inscrire dans `CONFIG_MODEL_MAP` : Presidio valide le
fichier de reconnaisseurs par nom de classe, et sans cela les champs du
reconnaisseur Hugging Face (`model_name`, `label_mapping`, `threshold`…) sont
ignorés pour une sous-classe. Enfin, le fichier de reconnaisseurs ne peut nommer
qu'une classe déjà importée : c'est le rôle de `analyzer_app.py`, qui remplace
`app` dans la commande de démarrage de l'image de base.

Ces points s'appuient sur des noms internes de Presidio. Le module les vérifie à
l'import : s'il en manque un, la construction échoue et l'image ne démarre pas.

`recognizer_check.py` contrôle le découpage (sur des textes aléatoires avec et
sans blancs, et sur des noms de plusieurs mots placés autour des coupes), la
réunion des trouvailles, le filtre des étiquettes et l'erreur d'inférence, avec
un faux modèle. La construction l'exécute, et il reste dans l'image : il se
relance sur une image construite, sans réseau ni modèle chargé :

```bash
docker run --rm --network none --entrypoint python <image> -B /app/recognizer_check.py
```

Pour le lancer pendant qu'on modifie `mapped_ner_recognizer.py`, sans construire
l'image :

```bash
docker run --rm --network none -e PYTHONPATH=/app \
  -v "$PWD/docker/presidio-fr":/src:ro -w /src --entrypoint python \
  mcr.microsoft.com/presidio-analyzer:2.2.362 -B recognizer_check.py
```

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

`--build` compte : sans lui, `docker compose up` ne construit l'image que si
elle n'existe pas. Une image locale construite avant l'arrivée de CamemBERT, ou
avant une modification de ce dossier, serait relancée telle quelle — sans la
configuration, qui n'est plus montée.

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
le code de gunicorn 25.2.0. Mesuré sur cette image, un démarrage à la fois sur
un CPU : 5 démarrages bloqués sur 53 sans l'option, 0 sur 60 avec. Le blocage a
aussi été observé sur l'image précédente de ce dossier lancée sous
l'utilisateur 1001 (1 démarrage sur 30), et pas lancée en root (0 sur 60) ; on
ne sait pas pourquoi il ne se montre pas en root. Ce socket ne sert pas ici (il
pilote gunicorn par la commande `gunicornc`). L'option est à retirer quand
l'image de base embarquera gunicorn 25.2.0 ou plus.

## Versions épinglées

Ce que le `Dockerfile` installe nommément y est fixé :

| Élément | Valeur | Où |
|---|---|---|
| Image de base | `mcr.microsoft.com/presidio-analyzer:2.2.362`, par étiquette **et** par empreinte | `FROM` |
| Modèle spaCy français | `fr_core_news_md-3.8.0` | `ARG SPACY_FR_MODEL` |
| PyTorch (CPU seul) | `2.14.1+cpu` | `ARG TORCH_VERSION` |
| transformers | `5.19.0` | `ARG TRANSFORMERS_VERSION` |
| sentencepiece, protobuf | `0.2.2`, `7.36.2` | `ARG SENTENCEPIECE_VERSION`, `ARG PROTOBUF_VERSION` |
| Modèle CamemBERT | `Jean-Baptiste/camembert-ner`, commit `ef35fe7767c1dad71f5c853838cdd80d0b3441ed`, poids au seul format safetensors | `ARG CAMEMBERT_REVISION` |

**Ce qui ne l'est pas** : les dépendances que ces paquets entraînent
(`huggingface_hub`, `hf_xet`, `tokenizers`, `safetensors`, `fsspec`, `networkx`,
`sympy`, `mpmath`) prennent la version que pip résout le jour de la
construction. Deux paquets de l'image de base sont aussi montés par ces
dépendances (`click`, `idna`). Deux constructions à des dates différentes
peuvent donc différer sur ces paquets.

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
| Démarrage jusqu'à `/health` | 8 à 10 s | 6,5 s |
| 2 000 caractères, sans quota | 0,56 s | 0,07 s |
| 2 000 caractères, `--cpus 2` | 0,53 s | — |
| 2 000 caractères, `--cpus 2` et `OMP_NUM_THREADS` non fixé | 1,66 s | — |

Un texte de 5 000 caractères sans aucun blanc prend environ 3 s sans quota.

## Reconnaissance mesurée sur des textes métier

La mesure est `internal/integration-tests/presidio/measure_integration_test.go`,
sur les jeux inventés de `testdata/free-text-fr.json` et `free-text-en.json` :
12 colonnes de 50 valeurs par langue (6 colonnes qui nomment des personnes, 6
qui n'en nomment pas : noms de sociétés, de produits, villes, codes). Chaque
valeur est tronquée à ses 200 premiers caractères et envoyée seule, avec
`score_threshold` à 0,35. Un passage désigné est juste quand il chevauche un nom
annoté de sa valeur ; un nom est trouvé quand un passage `PERSON` le chevauche.
Un titre (« Mme », « M. », « Mr ») n'est pas compté dans le nom annoté. Ce sont
des chiffres de ces jeux, une mesure par cas : pas une garantie sur d'autres
textes. Les jeux comptent 32 noms dans chaque langue.

| | Français | Anglais |
|---|---|---|
| Passages `PERSON` rendus | 32 | 48 |
| Passages justes, précision | 31, soit 0,97 | 30, soit 0,63 |
| Noms trouvés, rappel | 31 sur 32, soit 0,97 | 30 sur 32, soit 0,94 |
| Valeurs désignées dans les colonnes sans personne | 1 sur 300 | 16 sur 300 |

En français, les 31 passages justes ont exactement les bornes du nom annoté. Sur
les mêmes valeurs et le même seuil, l'image d'avant CamemBERT désignait 32
valeurs des colonnes sans personne (mesure par valeur faite lors de la mise au
point, non rejouable avec ce dépôt). L'anglais n'a pas changé : ses chiffres sont ceux de l'image de base.

**Ce que la mesure a manqué ou désigné à tort, à lire comme des limites :**

- français, un nom manqué : un nom de famille en capitales en tête de valeur
  (« LEMOINE Sophie a remplacé le capteur… ») n'est pas rendu, alors que
  « BESSON Thibault » l'est : deux cas, trop peu pour fixer une règle ;
- français, un passage à tort : un nom de cabinet (« Cabinet Oréade »), pris
  pour une personne ;
- français, un titre (« Mme ») placé devant le nom n'a pas été dans le passage
  désigné sur ces jeux : qui réécrit le passage laisse le titre en place ;
- anglais, deux noms manqués (« Grace Barnes » dans une remarque de commande,
  « Draper » après « Mr ») ; 18 passages à tort : noms de sociétés, un nom
  commun en début de phrase, numéros de commande (« SO-362398 »), un mot d'état
  (« Voicemail ») ;
- `LOCATION` vient toujours de spaCy en français, comme `DATE_TIME` et `NRP` :
  CamemBERT n'y change rien. Dans les colonnes françaises sans personne, des
  valeurs portent encore des types sensibles autres que `PERSON` (sur 300
  valeurs : 73 `LOCATION`, 30 `PHONE_NUMBER`, 9 `FR_PHONE_NUMBER`, 8
  `FR_SIRET`) ;
- le découpage des longs textes a ses propres limites, plus haut.

## Tests

`internal/integration-tests/presidio` construit l'image et l'interroge en HTTP.
Comme la construction télécharge PyTorch et le modèle, ces tests ne tournent que
sur demande :

```bash
PRESIDIO_IMAGE_TESTS=1 go test ./internal/integration-tests/presidio/... -count=1 -timeout 30m
```

**Ce qu'un lancement laisse sur la machine.** L'image construite reste sous le
nom `husonym-presidio-analyzer-test:latest` (3,9 Go), pour que le lancement
suivant réutilise ses couches. Un lancement fait après une modification de ce
dossier construit une nouvelle image sous ce nom et laisse la précédente sans
nom : jusqu'à 3,9 Go de plus à chaque fois si les premières couches ont changé.
Pour les retirer :

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
| PyTorch (`torch`) | Apache-2.0, Apache-2.0 avec exception LLVM, BSD-2-Clause, BSD-3-Clause, BSL-1.0 et MIT |
| `transformers`, `tokenizers`, `safetensors`, `huggingface_hub`, `hf_xet`, `sentencepiece` | Apache-2.0 |
| `protobuf`, `fsspec`, `networkx` | BSD-3-Clause |
| `sympy`, `mpmath` | BSD |
| `click`, `idna` (déjà dans l'image de base, montés ici) | BSD-3-Clause |
| `Jean-Baptiste/camembert-ner` | MIT (fiche du modèle, copiée dans l'image) |

L'image de base apporte Presidio Analyzer (MIT), spaCy (MIT) et le modèle
`en_core_web_lg` (MIT), avec leurs propres dépendances.
