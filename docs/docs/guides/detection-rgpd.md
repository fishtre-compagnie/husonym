---
title: Détection RGPD automatique
description: Comment Husonym analyse les tables d'une base source pour repérer les données personnelles et proposer un transformer d'anonymisation
id: detection-rgpd
hide_title: false
slug: /guides/detection-rgpd
---

## À quoi ça sert

Quand vous configurez un job, Husonym analyse les tables de la connexion source et
répond à deux questions pour chaque colonne :

1. **Est-ce une donnée personnelle ?** (au sens RGPD)
2. **Par quoi la remplacer ?** (transformer d'anonymisation adapté)

Le but est qu'une base de production ne parte jamais en clair vers un environnement
de test parce qu'une colonne est passée inaperçue.

## Ce que vous voyez à l'écran

Une colonne **RGPD** apparaît dans le tableau de mapping, avec trois états :

| Badge             | Signification                                     | Action attendue        |
| ----------------- | ------------------------------------------------- | ---------------------- |
| 🟢 **RGPD**       | Donnée personnelle, et un transformer l'anonymise | Rien                   |
| 🟠 **À vérifier** | Détection probable mais non prouvée               | Confirmer ou écarter   |
| 🔴 **Non traité** | Donnée personnelle qui partirait **en clair**     | Choisir un transformer |

L'infobulle donne le moteur qui a reconnu la colonne : _dictionnaire_,
_clé de contrôle_, _analyse de format_ ou _IA_.

Le bouton **œil** affiche les 20 premières valeurs de la colonne pour lever un doute
sans quitter la page.

## Les quatre moteurs de reconnaissance

L'analyse se fait en cascade : le premier moteur qui **prouve** quelque chose gagne,
et les suivants ne sont pas sollicités. L'ordre n'est pas arbitraire — il va du plus
certain au plus statistique.

### 1. Dictionnaire — le nom de la colonne

Le nom est lu mot à mot et comparé à un catalogue de mots-clés en huit langues —
français, anglais, allemand, espagnol, italien, néerlandais, polonais, portugais
(`email`, `prenom`, `telephone`, `date_naissance`, `nir`, `apellido`, `indirizzo`,
`woonplaats`…). La casse et les accents ne comptent pas : `PRÉNOM` se lit `prenom`,
et un mot allemand se lit avec son tréma ou avec les lettres qui le remplacent
(`staatsangehörigkeit`, `staatsangehoerigkeit`). Un nom écrit sans séparateur est
découpé en ses mots quand les règles les connaissent tous (`customeremail`,
`dateofbirth`, `addressline1`, `lieunaissance`, `passwordresettoken`) ; deux
dernières lettres peuvent suivre un mot-clé d'au moins six lettres (`postcodenl`).
Un nom qui contient un mot inconnu des règles est un autre mot, et n'est pas
signalé : `addressbook`, `streetview`, `pseudorandom`.

Un revenu, un salaire horaire et une naissance sont signalés comme ceux d'une
personne (`annual_income`, `employee_income`, `hourly_wage`, `birth_date`). À côté
d'un mot de la comptabilité ou de la statistique, ils ne le sont pas : `net_income`,
`gross_income`, `income_tax`, `revenu_fiscal`, `minimum_wage`, `birth_rate`. Le mot
d'une personne à côté d'eux en refait une détection (`employee_net_income`). Un
`salary` ou un `salaire` est toujours signalé.

C'est **déterministe** : même résultat à chaque introspection, aucune donnée n'est
lue.

C'est le seul moteur autorisé à **appliquer** un transformer automatiquement, et
uniquement sur une colonne encore en _Passthrough_ — un choix explicite n'est jamais
écrasé.

La suggestion tient compte du **type SQL** : un téléphone en `bigint` reçoit
`Generate Int64 Phone Number` et non sa variante texte ; une date de naissance en
type `date` natif reçoit un générateur de timestamp, alors que la même date stockée
en `varchar` reçoit `Transform Character Scramble` (voir _Le cas des dates_ plus bas).

**Une colonne reconnue sensible a un transformer suggéré quand un transformer écrit
son type** : texte, entier, décimal, booléen, date ou horodatage, UUID. Quand la
donnée n'a pas de générateur propre (mot de passe, jeton, clé, identifiant national,
IBAN, compte bancaire, salaire, âge, origine ethnique, situation de famille, adresse
MAC, carte bancaire en texte, date de naissance en texte), la suggestion est :

| Type de la colonne | Transformer suggéré            | Ce qu'il écrit                                                                                |
| ------------------ | ------------------------------ | --------------------------------------------------------------------------------------------- |
| Texte              | `Transform Character Scramble` | Même longueur ; une lettre devient une lettre, un chiffre un chiffre, un signe un autre signe |
| Entier             | `Generate Random Int64`        | Un entier tiré au hasard                                                                      |
| Décimal            | `Generate Float64`             | Un décimal tiré au hasard                                                                     |
| Booléen            | `Generate Boolean`             | Vrai ou faux, au hasard                                                                       |
| Date, horodatage   | `Generate UTC Timestamp`       | Un horodatage tiré au hasard                                                                  |
| UUID               | `Generate UUID`                | Un UUID tiré au hasard (seul un secret est reconnu dans une colonne de ce type)               |

Un générateur propre à la donnée qui écrit des nombres de dix chiffres ou plus
(`Generate Int64 Phone Number`, `Generate Card Number`) n'est suggéré que pour un
`bigint` ; un entier plus étroit reçoit `Generate Random Int64`.

**Une colonne sensible dont aucun transformer n'écrit le type n'a pas de
suggestion** : JSON, tableau, binaire (`bytea`, `varbinary`), `inet`, `enum`, `set`,
domaine ou tout autre type défini dans le schéma. Elle reste signalée comme donnée
personnelle, reste en _Passthrough_ tant que personne ne choisit un transformer, et
le badge indique qu'aucun transformer compatible n'existe.

Une date de naissance stockée en texte reçoit `Transform Character Scramble` : le
texte écrit n'est plus une date (voir _Le cas des dates_).

**Valeurs NULL.** Un générateur (`Generate …`) ne lit pas la valeur qu'il remplace :
il écrit une valeur aussi à la place d'un NULL. C'est le cas des suggestions pour un
salaire, un âge, un genre, une date de naissance native, un téléphone ou un code
postal stockés en nombre. `Transform Character Scramble` garde un NULL, et sous
Athanor les transformers cohérents listés ci-dessous le gardent aussi.

**Cohérence.** Les générateurs tirent leurs valeurs au hasard : deux lignes de même
valeur ne reçoivent pas la même sortie. Sous Athanor, quand le compte a une clé de
cohérence, les transformers suivants donnent la même sortie pour la même entrée dans
toute la portée de cohérence, et aucun autre :

- `Generate First Name`, `Transform First Name`, `Generate Last Name`,
  `Transform Last Name`, `Generate Full Name`, `Transform Full Name` ;
- `Generate City`, `Generate State`, `Generate Zipcode`, `Generate Street Address`,
  `Generate Country`, `Generate Business Name` ;
- `Generate Email`, `Transform Email` ;
- `Transform Phone Number`, `Transform E164 Phone Number`,
  `Generate E164 Phone Number` ;
- `Transform Character Scramble`.

`Generate Full Address`, `Generate Username`, `Generate Gender`, `Generate SSN`,
`Generate IP Address` et les générateurs de nombres, de booléens, de dates et d'UUID
ne sont pas cohérents. Sans clé de cohérence, aucun transformer ne l'est.

Sous Athanor avec une clé, `Transform Character Scramble` tire chaque caractère à
partir de la valeur entière : deux valeurs égales donnent la même sortie dans toutes
les colonnes, deux valeurs qui diffèrent d'un caractère donnent des sorties sans
rapport. La sortie a autant de caractères que l'entrée ; une lettre, accentuée ou
non, devient une lettre ASCII de même casse, un chiffre un chiffre ASCII, un signe de
la liste `!@#$%^&*()-+=_ []{}|\;"<>,./?` un autre signe de cette liste ; les espaces
et les autres signes sont gardés. La sortie n'est jamais l'entrée elle-même, sauf
quand la valeur ne contient aucun caractère à tirer. Deux valeurs différentes peuvent
donner la même sortie, d'autant plus souvent qu'elles sont courtes.

### Les nouvelles colonnes d'un run (AutoMap)

Avec la stratégie _AutoMap_, un run applique la suggestion du dictionnaire aux
colonnes apparues dans la source depuis le dernier run, et les signale pour revue.
Trois cas restent en _Passthrough_ :

- la colonne porte une **clé** — clé primaire, clé étrangère réelle ou virtuelle,
  contrainte ou index unique — ou est référencée par une clé étrangère ;
- la colonne est nommée par une **contrainte CHECK** de sa table : une valeur
  réécrite ne la satisferait pas et le run s'arrêterait. Le catalogue donne ces
  contraintes pour PostgreSQL et MySQL ; pour SQL Server elles ne sont pas lues, et
  la colonne est réécrite ;
- **aucun transformer n'écrit son type**, ou son type n'est pas connu du run.

L'avertissement du run compte les colonnes laissées telles quelles qui sont des
données personnelles, et nomme chacune avec sa catégorie et la raison :

```text
3 unmapped columns passed through as is, awaiting review, 2 of them personal data
(named with the category and the reason): [public.people.dob (birth_date, under a
CHECK constraint), public.people.notes, public.people.token (secret, covered by a key)]
```

La plage d'un nombre généré tient dans la colonne : 18 à 90 pour un âge, 20 000 à
90 000 pour un salaire, coupée à ce que le type contient (`smallint` : 32 767 ;
`numeric(4,2)` : 99). Quand le bas de la plage n'y tient pas non plus, elle part de
zéro.

### 2. Clés de contrôle — la valeur se vérifie

Sur un échantillon de 20 lignes, certaines valeurs peuvent être **vérifiées
mathématiquement**, pas seulement reconnues de vue :

| Donnée                                    | Contrôle                                 |
| ----------------------------------------- | ---------------------------------------- |
| NIR (n° sécurité sociale)                 | clé = 97 − (13 premiers chiffres mod 97) |
| IBAN                                      | clé mod 97 (ISO 13616)                   |
| SIRET / SIREN                             | Luhn                                     |
| Carte bancaire                            | Luhn + préfixe réseau                    |
| Email, adresse IP, téléphone FR, civilité | forme strictement contrainte             |

Une colonne est classée **Confirmée** si 90 % de ses valeurs valident, **À vérifier**
entre 50 % et 90 %.

**Pourquoi cet étage passe avant l'IA :** Presidio classe le NIR `180017511600146`
en _carte bancaire_ avec un score de **1.00** — ce NIR passe Luhn par hasard. Un
score maximal sur la mauvaise catégorie ne se corrige pas en ajustant un seuil. Les
validateurs sont donc ordonnés par **spécificité** : le NIR (structure
sexe/année/mois/département **et** mod 97) est plus contraint qu'un simple Luhn, il
est évalué avant la carte bancaire.

### 3. Analyse de format — les dates

Voir la section dédiée ci-dessous.

### 4. IA (Presidio) — ce qui ne se prouve pas

Reste ce qu'aucune règle ne peut trancher : noms de personnes, villes, adresses,
texte libre. Husonym interroge un
[Presidio Analyzer](https://microsoft.github.io/presidio/) **auto-hébergé** : les
données ne quittent jamais votre infrastructure.

Un résultat d'IA est **toujours** marqué _À vérifier_ et **ne modifie jamais un
transformer** — un modèle statistique ne prouve rien, il alerte.

Deux ajustements maison :

- **Catalogue français.** L'image officielle ne connaît que l'anglais et n'a aucun
  reconnaisseur France. Nous ajoutons le modèle spaCy `fr_core_news_md` et les
  reconnaisseurs `FR_NIR`, `FR_PHONE_NUMBER`, `FR_POSTAL_CODE`, `FR_SIRET`
  (voir `docker/presidio-fr/`).
- **Affinage local.** Presidio dit `PERSON` pour un prénom, un nom ou un nom complet
  indifféremment, et `LOCATION` pour une ville comme pour une adresse. Husonym
  tranche ensuite sur la forme des valeurs : plusieurs mots → nom complet ; numéro en
  tête **et** type de voie → adresse, sinon ville.

## Le cas des dates

Une colonne typée `DATE`/`TIMESTAMP` n'a pas de format : le driver renvoie une valeur
normalisée. Le problème ne se pose que pour les dates stockées en **texte**, cas
hérité mais fréquent.

Husonym infère le format **au niveau de la colonne** (elle est homogène) : un format
n'est retenu que s'il explique **toutes** les valeurs. `25/12/1980` élimine
mécaniquement `mm/jj/aaaa`.

Quand plusieurs formats survivent — toutes les valeurs ayant jour **et** mois ≤ 12 —
c'est indécidable par les données. Husonym **ne devine pas** : la colonne passe en
🟠 _À vérifier_ avec la mention « jj/mm/aaaa ou mm/jj/aaaa ».

L'enjeu n'est pas cosmétique : si la source contient `25/12/1980` et qu'on écrit
`1985-03-14`, l'application qui relit la base cible ne parse plus rien.

Aucun générateur ne sait restituer une date dans son format d'origine. **Une date de
naissance en texte reçoit donc `Transform Character Scramble`** : la longueur est
gardée, chaque chiffre est remplacé par un chiffre et chaque séparateur par un signe
de ponctuation. La valeur écrite n'est pas une date (`83!47&2916`) : si l'application
cible relit cette colonne comme une date, choisissez un autre transformer avant le
run.

Les mois en lettres (`25 decembre 1980`, accentué ou non) sont reconnus et, par
construction, non ambigus.

Enfin, une date n'est personnelle que si **son nom** l'indique : `created_at` n'est
pas une donnée RGPD, `date_naissance` l'est.

## Deux niveaux de confiance, pas un score

| Niveau         | Sens                                                                 | Effet                              |
| -------------- | -------------------------------------------------------------------- | ---------------------------------- |
| **Confirmé**   | Preuve déterministe (nom de colonne, clé de contrôle, format prouvé) | Badge vert, transformer applicable |
| **À vérifier** | Indice non prouvé (IA, format ambigu, clé partielle)                 | Badge orange, aucune modification  |

L'arbitrage entre deux détections concurrentes se fait par **spécificité de la
preuve**, jamais par score.

Un cas mérite d'être connu : le nom peut confirmer la **nature** de la donnée pendant
que le format reste douteux. « C'est bien une date de naissance » n'implique pas « on
sait dans quel format la réécrire ». Une colonne `date_naissance` au format ambigu
reste donc orange.

## Quand le scan se déclenche

- **À l'introspection** (dictionnaire) : automatiquement, à l'affichage du schéma.
- **Scan de contenu** (clés, format, IA) : automatiquement à l'ouverture d'un job,
  une seule fois, ou à la demande via le bouton **Scanner le contenu**.

Le scan lit **20 lignes par table**. Il n'écrase jamais un transformer déjà choisi.

Avec PostgreSQL et SQL Server, les lignes d'une table de plus de 1000 lignes sont tirées
de pages prises au hasard sur toute la table : une cinquantaine de pages, ou les pages qui
contiennent environ 1000 lignes quand c'est davantage ; une table de moins de cinquante
pages est lue en entier. Avec MySQL et MariaDB, quand la clé primaire tient en une seule
colonne de type entier, elles sont tirées de dix plages de la clé qui se suivent de sa
plus petite à sa plus grande valeur, jusqu'à 100 lignes consécutives dans chacune. Dans
tous les autres cas (avec PostgreSQL et SQL Server, table de 1000 lignes ou moins ; table
PostgreSQL jamais analysée, ou partitionnée sans aucune partition analysée ; table MySQL
ou MariaDB dont la clé primaire est d'une autre nature, dont les valeurs s'étendent sur
moins de 1000, ou dont les dix plages contiennent ensemble moins de 500 lignes), elles
sont tirées parmi les 1000 premières lignes de la table ; et quand le tirage sur toute la
table échoue ou rend moins de lignes que demandé, les lignes manquantes sont prises parmi
ces 1000 premières lignes, où une ligne déjà lue peut être relue. Deux scans de la même
table peuvent lire des lignes différentes.

## Configuration

Le scan de contenu nécessite Presidio. Sans lui, les moteurs 1 à 3 continuent de
fonctionner — la dégradation est gracieuse, seule l'IA disparaît.

```yaml
# compose.dev.yml
api:
  environment:
    - PRESIDIO_ANALYZER_URL=http://presidio-analyzer:3000
    - PRESIDIO_DEFAULT_LANGUAGE=fr
```

:::note Pourquoi `fr` alors que Presidio est meilleur en anglais
Mesuré **isolément**, le modèle français est moins bon (F1 0,47 contre 0,63). Mesuré
**dans le pipeline**, il gagne (F1 0,90 contre 0,88). La raison : sur une colonne
d'adresses, Presidio émet `LOCATION` en français et _rien_ en anglais — et l'affinage
Husonym sait reclasser ce `LOCATION` en adresse. Un signal mal étiqueté qu'un étage
aval corrige vaut mieux qu'aucun signal.
:::

## Performances mesurées

Sur un jeu de test de 34 colonnes françaises à vérité terrain connue
(`scripts/testdata/`) :

| Configuration                   | Rappel   | Précision | F1       |
| ------------------------------- | -------- | --------- | -------- |
| Presidio seul, image officielle | 71 %     | 73 %      | 0,72     |
| **Pipeline Husonym complet**    | **84 %** | **96 %**  | **0,90** |

**Zéro faux positif** — aucune colonne anodine n'est signalée à tort. C'est le
chiffre qui compte le plus : un signal qui se déclenche à tort dégrade la confiance
dans tous les autres.

Le banc est rejouable :

```bash
python3 scripts/testdata/bench-presidio.py
```

## Limites connues

- **Texte libre multi-PII** — une colonne de commentaires contenant à la fois un nom
  et un téléphone ne remonte qu'une seule catégorie, la plus fréquente.
- **Prénom vs nom sur un mot seul** — indistinguables par le contenu sans dictionnaire
  INSEE. Le nom de colonne, lui, tranche sans ambiguïté.
- **Code postal par le contenu** — volontairement absent : « 5 chiffres, département
  01-98 » décrit aussi un salaire (`28000`). Détecté par le nom de colonne.
- **Date de naissance sur colonne anonyme** (`col_6`) — une date sans nom parlant est
  indécidable.

## Où c'est implémenté

| Fichier                                     | Rôle                                               |
| ------------------------------------------- | -------------------------------------------------- |
| `backend/pkg/piidetect/piidetect.go`        | Dictionnaire nom + type → catégorie et transformer |
| `backend/pkg/piidetect/validate.go`         | Clés de contrôle (NIR, IBAN, Luhn, civilité)       |
| `backend/pkg/piidetect/dateformat.go`       | Inférence de format de date                        |
| `backend/pkg/piidetect/refine.go`           | Affinage des sorties Presidio                      |
| `backend/services/.../pii-detect.go`        | Orchestration de la cascade                        |
| `docker/presidio-fr/`                       | Image Presidio francisée                           |
| `frontend/.../JobMappingTable/RgpdCell.tsx` | Badge et infobulle                                 |
| `frontend/.../SchemaTable/SchemaTable.tsx`  | Scan automatique, application des suggestions      |
