# Règles de transformation personnalisées

Statut : **validée le 2026-09-18**, implémentation en cours sur `feat/athanor-moteur-m1-m3` (décisions en
fin de document). Prolonge la décision 7 de [athanor-plan-neutre.md](athanor-plan-neutre.md) ; les cas
et les mesures vont dans [banc-essai-moteurs.md](banc-essai-moteurs.md).

## Le besoin

Offrir à l'utilisateur un moyen d'écrire sa propre règle quand aucun transformer tout fait ne convient.
C'est le rôle des transformers JavaScript (`TransformJavascript`, `GenerateJavascript`). Sous Athanor,
une règle personnalisée doit donner au moins ce que donne un transformer tout fait : la **cohérence**
(même valeur d'entrée, même sortie, dans la portée du job), sans jamais faire passer la donnée d'une
personne dans la ligne d'une autre.

## Ce que fait le code aujourd'hui (vérifié le 2026-09-18, par test)

- **L'état d'une ligne passe à la suivante.** Un script qui écrit `neosync.last = value` sur une ligne
  et le relit sur la suivante rend, pour Bob, le nom réel d'Alice. Athanor garde une VM par page
  (`newJavascriptRows`, appelé par `SpecForTable` à chaque page), Benthos une VM tirée d'un
  `sync.Pool` : l'état survit au gré des pages ou du pool, jamais de façon fiable.
- **Les fonctions offertes aux scripts sont aléatoires** (`husonym.*`, les transformers Benthos) : une
  règle personnalisée perd la cohérence que les transformers natifs d'Athanor tiennent de
  `consistency.Deriver` (`runner/deterministic.go`).
- **Un script lit les fichiers de la machine qui l'exécute.** La VM active `require()` avec le chargeur
  par défaut de goja_nodejs, qui lit le système de fichiers : `require("/chemin/fichier.json")` rend
  le contenu d'un fichier du worker. Le même code s'exécute **dans l'API** : `AnonymizeSingle` /
  `AnonymizeMany` passent par `internal/json-anonymizer`, qui exécute les transformers JavaScript.
  Tout utilisateur d'un compte peut donc lire un fichier JSON ou JavaScript du conteneur de l'API.
- **Aucune limite de temps.** `Runner.Run` ignore son contexte : `while (true) {}` tourne encore
  après l'échéance du contexte. Sur le worker, l'activité reste bloquée jusqu'à son délai ; dans
  l'API, le gestionnaire ne rend jamais la main.
- **L'erreur ne nomme pas la colonne** : toutes les colonnes JavaScript d'une table forment un seul
  programme, et l'erreur rendue est « runner: exécution du JavaScript: … ».
- **La validation de l'UI ne fait que compiler** (`ValidateUserJavascriptCode`), en mode strict, alors
  que l'exécution compile en mode non strict : les deux ne jugent pas le même code.

## Mesures (2026-09-18)

### Coût par ligne (micro-banc, lots de 1 000 lignes, 4 colonnes)

| Chemin | Binaire optimisé | Binaire de débogage (`-N -l`, celui du worker de dev) |
|---|---|---|
| Colonne recopiée | 2 ns | 3 ns |
| Transformer natif déterministe (nom) | 0,65 µs | 1,6 µs |
| JavaScript `value.toUpperCase()` | 4,6 µs | 11,4 µs |
| JavaScript `husonym.generateLastName({})` | 6,7 µs | 16 µs |
| goja seul, même script | 0,95 µs | 1,8 µs |
| VM neuve (`NewDefaultValueRunner`) | 55 µs | 93 µs |
| Limite de temps par minuterie (`time.AfterFunc` + `Interrupt`) | +0,25 µs | +0,3 µs |

Une règle JavaScript coûte **7 fois** un transformer natif, et l'essentiel de ce coût n'est pas goja
(1 µs) mais la tuyauterie ligne → message Benthos → VM → message. Une VM neuve par ligne coûterait
**12 fois** l'exécution du script : ce n'est pas le moyen d'isoler les lignes.

### De bout en bout (`bench/perf`, échelle 100)

Même jeu, sans puis avec `return value.toUpperCase();` sur `COMMANDE.reference` (500 000 lignes
écrites), 5 tours par moteur, les deux séries d'un SGBD enchaînées dans la même heure. Durée
médiane (min – max) :

| SGBD | Moteur | Sans JavaScript | Avec JavaScript | Écart |
|---|---|---|---|---|
| MySQL | Athanor | 24,6 s (24,0 – 26,1) | 30,2 s (29,0 – 31,5) | **+5,6 s (+23 %)** |
| MySQL | Benthos | 161,3 s (151,0 – 169,3) | 169,9 s (167,2 – 180,2) | +8,6 s (+5 %) |
| PostgreSQL | Athanor | 32,3 s (30,9 – 34,5) | 38,4 s (37,2 – 38,8) | **+6,1 s (+19 %)** |
| PostgreSQL | Benthos | 171,0 s (159,1 – 175,8) | 177,1 s (174,9 – 182,4) | +6,1 s (+4 %) |

Chez Athanor, les plages ne se recouvrent pas : l'écart est dû au script. **5,6 s pour 500 000
lignes, soit 11,2 µs par ligne**, exactement ce que prédit le micro-banc sur le binaire de débogage
(11,4 µs) : le coût est linéaire et le micro-banc suffit à l'anticiper. Une seule colonne
JavaScript sur la plus grosse table coûte un cinquième du run ; chez Benthos, le même coût se perd
dans le reste.

**Conclusion : l'étape « expressions » n'est pas justifiée.** goja ne compte que pour 1 µs sur
4,6 ; le reste est la tuyauterie d'Athanor (ligne recopiée dans une `map`, puis dans un message
Benthos, relue, fusionnée). Passer la ligne à la VM sans message Benthos est la première piste, à
mesurer, sans rien changer au contrat.

**Les mesures de bout en bout du banc portent sur le binaire de débogage** : le worker de dev est
compilé avec `-gcflags="all=-N -l"` (`worker/dev/build/.air.toml`). Les rapports entre moteurs
restent comparables, les durées absolues ne sont pas celles de la production.

## Proposition

### 1. Contrat d'une règle (décidé le 2026-09-18)

Une fonction par ligne, qui reçoit la valeur de la colonne et la ligne source. L'état est partagé
entre les colonnes d'une ligne, **jamais transmis à la ligne suivante**.

**Mise en œuvre proposée** : dans le code partagé (`internal/javascript/vm`), donc pour les deux
moteurs et pour l'API, sans VM neuve par ligne :

- à la création de la VM, les objets intégrés (`Object.prototype`, `Array.prototype`, `Math`,
  `JSON`…) et l'objet des fonctions offertes sont gelés, et la liste des globaux est relevée ;
- avant chaque ligne, `husonym` et `neosync` redeviennent un objet vide qui hérite des fonctions :
  `neosync.x = …` reste possible d'une colonne à l'autre, et repart à vide à la ligne suivante ;
- après chaque ligne, tout global créé par la ligne est supprimé (`x = …` sans déclaration,
  `globalThis.x = …`).

Le coût de cette remise à zéro sera mesuré par le micro-banc avant d'être retenu.

### 2. Fonctions déterministes offertes aux scripts (`pseudo.*`)

Un espace de noms à part, `pseudo`, pour ne pas mêler fonctions aléatoires (`husonym.*`) et
déterministes. Chaque fonction
rend **exactement** ce que rend le transformer natif pour la même valeur, dans la même portée : elle
est construite à partir de la même configuration par `deterministicValueTransformer`, aucun
générateur n'est réécrit.

| Fonction | Transformer natif équivalent | Domaine de dérivation |
|---|---|---|
| `firstName(v)` | Generate/TransformFirstName | `person.first_name` |
| `lastName(v)` | Generate/TransformLastName | `person.last_name` |
| `fullName(v)` | Generate/TransformFullName | `person.full_name` |
| `email(v)` | Generate/TransformEmail (casse conservée) | `person.email` |
| `phone(v)` | Transform/Generate E164, TransformPhoneNumber | `person.phone` |
| `city(v)`, `state(v)`, `zipcode(v)`, `streetAddress(v)`, `country(v)` | Generate… | `geo.*` |
| `businessName(v)` | GenerateBusinessName | `company.name` |
| `hash(v, "domaine")` | — | `user:domaine` |
| `int(v, "domaine", min, max)` | — | `user:domaine` |
| `pick(liste, v, "domaine")` | — | `user:domaine` |

Les domaines des primitives sont préfixés (`user:`) : une règle ne peut pas retomber, sans le
vouloir, sur la graine d'un transformer natif. Un `null` rend `null`, comme les transformers natifs.

### 3. Garde-fous

- **Limite de temps par ligne** : `Interrupt` de la VM à l'échéance, et à l'annulation du contexte
  (fin de l'activité). Mesuré : +0,25 µs par ligne.
- **Aucun accès aux fichiers** : `require()` passe par un chargeur qui refuse tout fichier (le module
  `console` reste). Aucun accès réseau : goja n'en offre pas ; seule `transformPiiText` appelle
  l'API d'anonymisation, comme aujourd'hui.
- **Erreur explicite** : la colonne en cause, la table, et le rang de la ligne dans la page. Aucune
  valeur source dans le message : il finit dans l'historique Temporal, les logs et l'UI.
- **Avertissement** quand un script écrit une variable globale (`x = …` sans déclaration,
  `neosync.x = …`, `globalThis.x = …`) : cet état ne vit que le temps d'une ligne. Rendu par
  `ValidateUserJavascriptCode`, qui compile désormais le code comme l'exécution (mode non strict).

### 4. Coût

La colonne JavaScript de `bench/perf` reste dans le jeu : chaque mesure future la couvre.
Première piste : donner la ligne à la VM sans passer par un message Benthos (voir les mesures).

### 5. Essai d'une règle

L'utilisateur donne quelques lignes (au plus 20, un objet JSON par ligne) ; l'API les passe par les
règles JavaScript d'une table **par le chemin d'Athanor** (`SpecForTable`, la même VM, les mêmes
garde-fous) avec une **clé de dérivation jetable**, tirée au hasard à chaque appel : les valeurs rendues
ont la forme des vraies, jamais leur contenu, et aucune clé ne quitte le worker. La réponse rend les
lignes transformées, ou l'erreur (colonne, rang de la ligne), et les avertissements. Les lignes sont
celles que l'utilisateur tape, jamais lues dans la source : une page d'essai ne doit pas afficher de
données de production.

### 6. Hors périmètre sauf mesure qui l'exige

Expressions déclaratives exécutables par lot, greffons WebAssembly, séquences (`CLIENT-00001`…) : une
séquence ne sort pas d'une fonction déterministe par ligne, ce serait un transformer natif à part.

## Cas du banc

1. `js-no-state-across-rows` : une ligne sur deux pose un état, l'autre le lit ; sur cinq pages, aucune
   ligne ne rend la valeur d'une autre. Les deux moteurs.
2. `js-consistent-across-pages-and-tables` : les mêmes valeurs source en page 1, en page 3 et dans une
   seconde table donnent la même sortie. Nouvelle règle de vérification `consistent`.
3. `js-matches-native` : dans une même table, une colonne native et une colonne JavaScript sur les
   mêmes valeurs rendent la même sortie (nom, email avec casse, téléphone, entier au-delà de 2^53).
   Nouvelle règle `same_as_column`.
4. `js-infinite-loop` : le run échoue au bout de la limite, en nommant la colonne. Les deux moteurs.
5. `js-reads-host-file` : `require()` d'un fichier fait échouer le run sans rien lire.
6. Règle `not_in_source_set` sur toutes les colonnes pseudonymisées.

## Décisions (2026-09-18)

1. Espace de noms **`pseudo.`**.
2. Contrat et garde-fous dans le code partagé : **Benthos et l'API les reçoivent aussi**. Les fonctions
   `pseudo.*` sont **réservées à Athanor** ; un job Benthos qui les utilise est arrêté au démarrage du
   run (activité `run-privileges`), transformers définis par l'utilisateur compris.
3. Scripts existants qui s'appuient sur un état entre lignes : **avertissement** quand un script écrit
   une variable globale.
4. **Essai d'une règle** avec une clé jetable, dans ce chantier.
5. La faille de lecture de fichiers (worker et API) est corrigée **dans ce chantier**, en premier.

## Étapes

1. Garde-fous de la VM partagée : chargeur `require` qui refuse tout fichier, limite de temps par
   exécution et arrêt à l'annulation du contexte.
2. Cas du banc des garde-fous et du contrat (1, 4, 5), puis remise à zéro de l'état entre deux lignes
   (coût mesuré) et erreur qui nomme la colonne.
3. Fonctions `pseudo.*` sous Athanor, arrêt d'un job Benthos qui les utilise ; cas 2, 3 et 6.
4. Avertissement « variable globale » et validation compilée comme l'exécution.
5. Essai d'une règle : RPC et UI.
6. Mise à jour de la décision 7 de `athanor-plan-neutre.md` et de `banc-essai-moteurs.md`.
