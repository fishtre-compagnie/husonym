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

| Badge | Signification | Action attendue |
|---|---|---|
| 🟢 **RGPD** | Donnée personnelle, et un transformer l'anonymise | Rien |
| 🟠 **À vérifier** | Détection probable mais non prouvée | Confirmer ou écarter |
| 🔴 **Non traité** | Donnée personnelle qui partirait **en clair** | Choisir un transformer |

L'infobulle donne le moteur qui a reconnu la colonne : *dictionnaire*,
*clé de contrôle*, *analyse de format* ou *IA*.

Le bouton **œil** affiche les 20 premières valeurs de la colonne pour lever un doute
sans quitter la page.

## Les quatre moteurs de reconnaissance

L'analyse se fait en cascade : le premier moteur qui **prouve** quelque chose gagne,
et les suivants ne sont pas sollicités. L'ordre n'est pas arbitraire — il va du plus
certain au plus statistique.

### 1. Dictionnaire — le nom de la colonne

Le nom est comparé à un catalogue de mots-clés FR/EN (`email`, `prenom`, `telephone`,
`date_naissance`, `nir`…). C'est **déterministe** : même résultat à chaque
introspection, aucune donnée n'est lue.

C'est le seul moteur autorisé à **appliquer** un transformer automatiquement, et
uniquement sur une colonne encore en *Passthrough* — un choix explicite n'est jamais
écrasé.

La suggestion tient compte du **type SQL** : un téléphone en `bigint` reçoit
`Generate Int64 Phone Number` et non sa variante texte ; une date de naissance en
type `date` natif reçoit un générateur de timestamp, alors que la même date stockée
en `varchar` n'en reçoit aucun (voir *Le cas des dates* plus bas).

### 2. Clés de contrôle — la valeur se vérifie

Sur un échantillon de 20 lignes, certaines valeurs peuvent être **vérifiées
mathématiquement**, pas seulement reconnues de vue :

| Donnée | Contrôle |
|---|---|
| NIR (n° sécurité sociale) | clé = 97 − (13 premiers chiffres mod 97) |
| IBAN | clé mod 97 (ISO 13616) |
| SIRET / SIREN | Luhn |
| Carte bancaire | Luhn + préfixe réseau |
| Email, adresse IP, téléphone FR, civilité | forme strictement contrainte |

Une colonne est classée **Confirmée** si 90 % de ses valeurs valident, **À vérifier**
entre 50 % et 90 %.

**Pourquoi cet étage passe avant l'IA :** Presidio classe le NIR `180017511600146`
en *carte bancaire* avec un score de **1.00** — ce NIR passe Luhn par hasard. Un
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

Un résultat d'IA est **toujours** marqué *À vérifier* et **ne modifie jamais un
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
🟠 *À vérifier* avec la mention « jj/mm/aaaa ou mm/jj/aaaa ».

L'enjeu n'est pas cosmétique : si la source contient `25/12/1980` et qu'on écrit
`1985-03-14`, l'application qui relit la base cible ne parse plus rien.

C'est pourquoi **une date en texte ne reçoit aucun transformer suggéré** : aucun
générateur ne sait restituer la date dans son format d'origine. La colonne reste
🔴 avec la mention « aucun transformer compatible » — c'est un signalement, pas un
oubli.

Les mois en lettres (`25 decembre 1980`, accentué ou non) sont reconnus et, par
construction, non ambigus.

Enfin, une date n'est personnelle que si **son nom** l'indique : `created_at` n'est
pas une donnée RGPD, `date_naissance` l'est.

## Deux niveaux de confiance, pas un score

| Niveau | Sens | Effet |
|---|---|---|
| **Confirmé** | Preuve déterministe (nom de colonne, clé de contrôle, format prouvé) | Badge vert, transformer applicable |
| **À vérifier** | Indice non prouvé (IA, format ambigu, clé partielle) | Badge orange, aucune modification |

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
d'adresses, Presidio émet `LOCATION` en français et *rien* en anglais — et l'affinage
Husonym sait reclasser ce `LOCATION` en adresse. Un signal mal étiqueté qu'un étage
aval corrige vaut mieux qu'aucun signal.
:::

## Performances mesurées

Sur un jeu de test de 34 colonnes françaises à vérité terrain connue
(`scripts/testdata/`) :

| Configuration | Rappel | Précision | F1 |
|---|---|---|---|
| Presidio seul, image officielle | 71 % | 73 % | 0,72 |
| **Pipeline Husonym complet** | **84 %** | **96 %** | **0,90** |

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

| Fichier | Rôle |
|---|---|
| `backend/pkg/piidetect/piidetect.go` | Dictionnaire nom + type → catégorie et transformer |
| `backend/pkg/piidetect/validate.go` | Clés de contrôle (NIR, IBAN, Luhn, civilité) |
| `backend/pkg/piidetect/dateformat.go` | Inférence de format de date |
| `backend/pkg/piidetect/refine.go` | Affinage des sorties Presidio |
| `backend/services/.../pii-detect.go` | Orchestration de la cascade |
| `docker/presidio-fr/` | Image Presidio francisée |
| `frontend/.../JobMappingTable/RgpdCell.tsx` | Badge et infobulle |
| `frontend/.../SchemaTable/SchemaTable.tsx` | Scan automatique, application des suggestions |
