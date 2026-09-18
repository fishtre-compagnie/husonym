# Athanor : destinations multiples et SGBD différents (proposition)

Statut : **proposition, à valider** (point 7 de la reprise du 2026-09-18). Rien n'est codé.

## Ce que fait le code aujourd'hui (vérifié)

- Athanor refuse un job à plusieurs destinations (`athanor.go` : « le câblage initial en gère une
  seule ») et une source d'un autre SGBD que la destination (`homogeneousDialect`). Benthos accepte
  les deux : une sortie par destination, et des conversions à l'écriture par pilote
  (`worker/pkg/benthos/sql/processor_husonym_{pgx,mysql,mssql}.go` : JSON, tableaux PostgreSQL,
  `uuid`/`money`/`tsvector`, valeurs par défaut).
- `RunTablePage` prend **un** dialecte, qui sert à la fois à lire la page (requête du plan, déjà
  écrite pour la source) et à écrire (insertion, contrôle des parents, suspension des FK).
- L'upsert lit la clé primaire dans la **source** (`primaryKeyColumns`), juste tant que la
  destination a le même schéma.
- Les clés transformées sont publiées dans Redis par table, sans notion de destination.
- Le gestionnaire de schéma est choisi d'après la destination mais lit ses instructions de création
  dans la source : pour une source d'un autre SGBD, le DDL de la source serait rejoué sur la
  destination. **À vérifier sur un serveur avant tout** (l'UI empêche peut-être cette combinaison).

## Proposition

Deux étapes indépendantes, la première beaucoup plus petite.

### 7a. Plusieurs destinations du même SGBD que la source

Une page est **lue une fois, transformée une fois, écrite N fois** : les N destinations reçoivent
exactement les mêmes valeurs transformées (ce que deux runs Benthos ne garantissent pas avec des
transformers non déterministes), et la source n'est lue qu'une fois.

- Chaque destination écrit la page dans **sa propre transaction** (aucune transaction ne couvre deux
  serveurs). Après la validation d'une destination, l'activité l'inscrit dans ses **détails de
  battement de cœur** Temporal ; une nouvelle tentative de la même page les relit et saute les
  destinations déjà validées. Chaque destination reçoit donc la page **exactement une fois**, table
  sans clé comprise — mieux que le « do nothing » de Benthos, qui double une table sans clé.
- Les options d'écriture restent par destination (`onConflict`, `skipForeignKeyViolations`, vidage,
  triggers, droits : ces trois derniers le sont déjà).
- **Point délicat : les lignes écartées.** Le contrôle des parents peut écarter une ligne dans une
  destination et pas dans l'autre (une destination conservée, `truncateBeforeInsert` différent). La
  clé transformée publiée pour les filles doit alors dépendre de la destination : le magasin Redis
  prend la destination dans sa clé, et chaque destination suit ses propres écartements.
- Écriture des destinations l'une après l'autre (simple, mémoire bornée) ; en parallèle seulement si
  la mesure le justifie.

### 7b. Source et destination de SGBD différents (MySQL ↔ PostgreSQL)

- Le runner reçoit **deux dialectes** : celui de la source (requête de page, jeton de reprise) et
  celui de chaque destination (écriture, contrôle des parents, suspension des FK, upsert — dont la
  clé primaire est lue **dans la destination**).
- **Conversions explicites** : à l'ouverture de la page, les types des colonnes de destination sont
  lus une fois, et chaque colonne reçoit un convertisseur choisi dans une **matrice (type source,
  type destination)** écrite et testée. Une paire absente de la matrice fait échouer le run **avant
  toute écriture**, en nommant la colonne et les deux types — jamais une conversion silencieuse. Pour
  les cas sans équivalent, une règle à trancher avec toi : date zéro MySQL vers PostgreSQL (refus
  explicite ou `NULL` configuré), `BIT(n)` vers `boolean`/`bit varying`, `TINYINT(1)` vers
  `boolean`, `timestamptz` vers `DATETIME` (UTC imposé), tableaux et `jsonb` vers `JSON` MySQL.
- La logique de conversion de Benthos est reprise dans un paquet neutre, pas appelée : Athanor ne
  dépend d'aucun type Benthos (décision du plan neutre).
- Init de schéma entre SGBD différents : hors périmètre tant que le point du gestionnaire de schéma
  n'est pas vérifié ; la destination doit alors exister.

### Banc

- 7a : un troisième serveur de destination par SGBD, et un mode du banc qui donne deux destinations
  au même job. Cas : les deux destinations identiques ligne à ligne, une destination qui échoue une
  fois après la validation de l'autre (exactement une fois chacune), une destination conservée et
  l'autre vidée (écartements différents, clés transformées suivies par destination).
- 7b : un mode croisé (source MySQL, destination PostgreSQL, et l'inverse). L'oracle compare
  aujourd'hui le texte imprimé par le même SGBD ; en croisé, chaque famille de types déclare la
  valeur attendue telle que la destination l'imprime. Tous les cas de types y passent.

## Questions pour toi

1. Ton besoin réel : plusieurs destinations, SGBD différents, les deux ? Le test de recette est
   MySQL vers MySQL. Si 7b n'a pas d'usage prévu, je propose de le laisser à Benthos et de ne faire
   que 7a.
2. Pour 7b, les règles des paires sans équivalent (dates zéro surtout) : refus explicite partout,
   ou réglage par job ?
