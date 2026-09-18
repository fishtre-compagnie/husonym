# Banc d'essai des moteurs (Benthos / Athanor)

Statut : conception validée le 2026-09-17, amendée le même jour après un second challenge (défauts du calcul
partagé, intégrité, analyse de schéma, structure du banc). Implémentation du banc en cours dans `bench/`.
Référence visuelle : page « Pièges par SGBD » (schéma d'écriture par SGBD, structures de FK du banc, tableau des
particularités).

## Objectif

Un jeu de données reproductible qui déclenche volontairement chaque cas délicat, et un outil qui exécute les
deux moteurs puis vérifie automatiquement les résultats. Chaque moteur est comparé **à l'attendu**, pas seulement
à l'autre : un défaut commun aux deux ne doit pas passer inaperçu.

## Constats du premier test réel (job `demo-outil-franchise`, 2026-09-17)

- Athanor 41,6 s contre 2 min 56 s pour Benthos ; 55 tables identiques sur 66, 10 écarts dus à la source vivante.
- Athanor écrit 36 références orphelines (`COMMANDE_MONTEUR.ID_INTERVENTION_GROUPEE`, FK auto-référencée vers
  des lignes hors subset) : les FK sont coupées pendant l'écriture. Benthos les laisse à `NULL` via
  `skipForeignKeyViolations`.
- Les deux moteurs excluent les commandes sans commande Allopneus : la FK nullable `ID_COMMANDE_ALLOPNEUS` est sur
  le chemin du subset, traduite en `INNER JOIN`.
- Le chemin de subset ne garde que le plus court chemin et la première FK trouvée entre deux tables : une FK
  obligatoire hors chemin peut produire des orphelins (cas « diamant »).
- Filtre des FK nullables mesuré sur la source : `EXISTS` ou `x IS NOT NULL AND x IN (…)` coûtent ~0 ;
  `x IN (…)` seul déclenche un parcours complet de table (sémantique de `NULL IN`).

## Défauts du calcul partagé relevés à la lecture du code (2026-09-17)

Ils touchent les deux moteurs. Chacun devient un cas du banc avant d'être corrigé.

- **`OR` de premier niveau dans un `WHERE` de subset** : les conditions sont ajoutées sans parenthèses
  (`query.Where(goqu.L(cond))`, `select-query-builder/querybuilder.go`). Vérifié sur goqu :
  `WHERE (t.a = 1 OR t.b = 2 AND (t.id > ?))`. La pagination relit les lignes `a = 1` ; combiné à une jointure de
  subset ou à une seconde racine, des lignes hors subset passent. Pas encore rejoué sur le builder complet.
- **« Do nothing » MySQL = `INSERT IGNORE`** (goqu), activé par les deux moteurs à chaque nouvelle tentative :
  masque la troncature, le `NOT NULL` remplacé par le défaut implicite, l'`ENUM` invalide et la collision sur
  n'importe quelle clé unique. Piste : `ON DUPLICATE KEY UPDATE pk = pk`.
- **Table sans clé et reprise** : « do nothing » n'a aucune clé sur laquelle entrer en conflit, la page est réécrite
  en double ; des lignes strictement identiques ne se paginent pas par curseur.
- **Colonnes de tri de repli** (`runconfigs/builder.go`, `getOrderByColumns`) : premier index unique, même
  nullable, puis toutes les colonnes triées par nom (`TEXT`, `JSON`, nullables comprises). Une table sans clé
  fiable doit être lue en un seul flux.
- **Qualification du `WHERE` MySQL** : seul le membre gauche des comparaisons est qualifié ; `IS NULL`, `BETWEEN`,
  `DATE(col) = …` donnent une colonne ambiguë sous jointure (échec franc).

Propres à Athanor, à confirmer par le banc : `BIT` et `GEOMETRY` absents de `binaryDatabaseTypes` (convertis en
`string`) ; si `SET FOREIGN_KEY_CHECKS=1` échoue, la connexion retourne au pool FK coupées (`Rollback` ne rétablit
pas une variable de session) ; une FK auto-référencée `NOT NULL` semble rejetée comme dépendance circulaire.

## Conception de l'intégrité référentielle d'Athanor (améliore Benthos)

1. **FK nullables** : filtre à la lecture dans la source (`CASE WHEN EXISTS (sélection du parent) THEN col END`),
   sous-requête avec alias propres, sans `ORDER BY` ni `LIMIT`. Ne modifie jamais les lignes retenues, donc aucun
   cycle possible.
2. **FK obligatoires** : ligne écartée à l'écriture si son parent est absent de la destination (lecture indexée par
   lot). Les parents sont écrits avant (dépendances du workflow), la cascade est naturelle.
3. **Filet de fin de run** : comptage des orphelins sur toutes les FK des tables du job, virtuelles comprises.
   Avec `skipForeignKeyViolations` : `NULL` ou suppression en cascade ; sans : échec en listant les orphelins.

Les FK virtuelles sont traitées comme les réelles (Benthos écrit leurs orphelins).

Amendements validés le 2026-09-17 :

- **L'étape 3 est une réparation, pas un simple filet** : l'`EXISTS` de l'étape 1 s'appuie sur la sélection
  source du parent ; si l'étape 2 écarte ce parent, ou si la source change entre deux tables, la référence écrite
  est orpheline. L'étape 3 itère jusqu'à stabilité et journalise ce qu'elle modifie.
- **Elle ne répare que si la destination a été vidée par le run.** Sinon elle détecte et échoue : une réparation
  toucherait des lignes que le run n'a pas écrites.
- **L'étape 2 ne contrôle que les FK dont le parent est réduit** (par subset ou par écartement en cascade) ; le
  reste relève de l'étape 3. Le raccourci « la FK du chemin est garantie par la jointure » est faux avec deux
  racines (parent filtré par R1 et R2, enfant joint par R1 seulement).
- Clé primaire transformée : la FK est traduite via Redis **avant** le contrôle en destination.
- SQL Server : une seule revalidation `WITH CHECK`, en fin de run (par lot elle serait quadratique et échouerait
  sur un cycle), avec reprise si un worker tué laisse les contraintes désactivées.
- PostgreSQL : le contrôle des droits tente `SET LOCAL session_replication_role` dans une transaction annulée
  plutôt que de lire `rolsuper` (services managés sans vrai superutilisateur). Alternative à étudier en phase 3 :
  créer les FK après le chargement quand Husonym crée le schéma.
- Écarté : remonter les parents manquants (fermeture vers le haut), qui ferait entrer des données hors subset.

Précisions issues du challenge, vérifiées dans `worker/pkg/select-query-builder` et `internal/runconfigs` :

- Le subset suit, depuis chaque table à `WHERE`, **le plus court chemin** vers chaque table et **la première FK**
  trouvée entre deux tables, nullable ou non, traduite en `INNER JOIN`. Une FK hors chemin n'est jamais filtrée,
  même obligatoire (cas « diamant ») ; une FK auto-référencée n'est jamais sur un chemin.
- Le filtre ne doit toucher que les **valeurs projetées**, jamais les **lignes retenues** : c'est ce qui exclut
  toute récursion, même en cas de cycle. Un filtre qui retire des lignes rendrait les tables interdépendantes.
- La sous-requête reprend la sélection du parent avec des alias propres (le builder nomme la racine par le nom de
  la table : collision en auto-référence), sans `ORDER BY` (refusé par SQL Server sans `TOP`) ni `LIMIT` (refusé
  par MySQL dans `IN`). FK composite : `EXISTS` (SQL Server refuse `(a, b) IN`). Sémantique `MATCH SIMPLE` : une
  colonne déjà `NULL` suffit à satisfaire la contrainte ; ne mettre à `NULL` que les colonnes nullables.
- Mesure sur la source réelle (`COMMANDE_MONTEUR`) : sans filtre 67 ms ; `EXISTS` 61 ms ; `x IS NOT NULL AND x IN`
  62 ms ; `x IN` seul ~0,5 s (parcours complet pour la sémantique de `NULL IN`). Retenu : `EXISTS`.
- Sans `skipForeignKeyViolations`, Benthos échoue : Athanor doit détecter et échouer, pas annuler en silence.
- Écart commun aux deux moteurs à corriger dans le builder partagé : une FK nullable sur le chemin du subset
  (`INNER JOIN`) exclut les lignes où elle vaut `NULL`.

## Contrôle des droits selon le rôle de la connexion

Décidé le 2026-09-17. Le test de connexion actuel (`CheckConnectionConfig`) ignore le rôle : il vérifie que la
connexion aboutit et liste les droits table par table. Le rôle dépend de l'usage (une connexion peut être source
d'un job et destination d'un autre) : on contrôle le couple connexion + rôle, et le moteur pour la destination.

| Contrôle | Source | Destination |
|---|---|---|
| Serveur accessible en écriture | non requis (réplica accepté) | bloquant : MySQL `@@read_only` / `@@super_read_only`, PostgreSQL `pg_is_in_recovery()`, SQL Server `DATABASEPROPERTYEX('Updateability')` et réplica secondaire |
| Lecture des tables du job | bloquant | — |
| Lecture des métadonnées de FK | bloquant (PostgreSQL masque dans `information_schema` les contraintes des tables sans droit : subset faux sans erreur) | — |
| Écriture et DDL | — | bloquant |
| Suspension des FK (Athanor) | — | bloquant : superutilisateur PostgreSQL (ou `GRANT SET ON PARAMETER`, PG 15+), `ALTER` SQL Server |
| Longue lecture sur réplica PostgreSQL | avertissement (`max_standby_streaming_delay`) | — |

Trois moments : test de connexion dans l'UI (choix source / destination, état par contrôle, explication et
`GRANT` à exécuter) ; création ou modification d'un job ; **démarrage d'un run, qui s'arrête** sur un contrôle
bloquant. `CheckConnectionConfig` reçoit le rôle et le moteur et renvoie une liste de contrôles, en conservant la
liste de droits existante. Scénarios du banc : destination sans droits, source sur un réplica.

## Analyse du schéma avant run

Décidé le 2026-09-17 : **analyse statique uniquement, produite par le calcul du plan, après le banc.**

- Les constats sont un sous-produit de `GenerateBenthosConfigs`, qui charge déjà colonnes, clés et index : un
  analyseur séparé qui redérive le chemin de subset finirait par diverger du moteur.
- Aucun scan de données sur la source avant run (anti-join par FK = parcours complets sur une base partagée) :
  l'étape 2 traite les orphelins de la source, l'étape 3 donne les chiffres réels sur la destination.
- Un seul rapport de pré-vol (droits, structure, transformers), une seule échelle (bloquant, avertissement,
  information), les trois moments du contrôle des droits. La détection RGPD reste distincte ; le pré-vol peut
  utiliser son résultat (« colonne PII en passthrough »). N'afficher que ce qui concerne les tables du job.

Proposition d'origine : un rapport produit avant le premier run (et à la configuration du job) qui détecte ce que
le banc teste : colonnes de tri non uniques ou nullables, orphelins déjà présents dans la source, auto-références
et cycles, chemins de subset en « diamant », FK nullables sur le chemin du subset, associations polymorphes sans
FK (candidates à une FK virtuelle), collations ou types divergents entre FK et parent, types non gérés, colonnes
générées, triggers en destination, transformers risqués (constante sur colonne unique, sortie plus longue que la
colonne). À challenger : périmètre, coût sur une grosse base de production, recoupement avec le contrôle des
droits et la détection RGPD existante.

## Cas à couvrir

Priorité **P1** : perte ou fuite de données, corruption silencieuse. **P2** : échec franc ou écart de comportement.
**P3** : robustesse et performance.

### Pagination et reprise (P1)

- Colonnes de tri **non uniques** (table sans clé, index non unique) : la pagination par curseur saute ou double
  des lignes à la frontière des pages.
- Colonnes de tri **nullables** (index unique sur colonne nullable) : `col > ?` exclut les `NULL`, lignes perdues.
- Valeurs de reprise mal restituées par le JSON du jeton : `BIGINT UNSIGNED` > 2^53 et `DECIMAL` (float64),
  `DATETIME` à microsecondes, clés binaires (`BINARY(16)`), collation insensible à la casse ou `PAD SPACE`.
- Nombre de lignes exactement égal à la taille de page (page vide finale) ; table vide ; une seule ligne.
- Source modifiée pendant le run (insertions avant le curseur, mises à jour de colonnes de tri).
- Tri sur plusieurs colonnes.
- **Table sans clé suivie d'une reprise** (« do nothing » n'a aucune clé sur laquelle entrer en conflit : page en
  double) ; **erreur masquée par `INSERT IGNORE`** lors d'une reprise ; **worker tué en cours de page** (remonté de
  P3). Ces trois cas demandent une page plus grande que le lot d'écriture et une panne provoquée.

### Subset et clés étrangères (P1)

- FK auto-référencée nullable vers des lignes hors subset ; hiérarchie profonde (arbre de 10+ niveaux).
- Cycle entre deux tables et cycle sur 3+ tables, entièrement nullables.
- Cas « diamant » : T → P1 et T → P2 obligatoires, filtrées par la même racine par deux chemins.
- FK nullable sur le chemin du subset (`INNER JOIN` qui exclut les lignes à `NULL`).
- Plusieurs FK vers le même parent (seule la première sert au subset).
- FK composite, partiellement nullable (`MATCH SIMPLE`) ; FK vers une colonne `UNIQUE` non clé primaire ; FK vers une
  partie d'une clé composite.
- FK vers une table **absente du job** ou d'un **autre schéma**.
- **Orphelins déjà présents dans la source** (écrits avec les FK coupées), dont la sentinelle legacy
  `id_parent = 0` sur une colonne `NOT NULL DEFAULT 0`.
- FK virtuelle sans contrainte en base ; association polymorphe (`entity_type` + `entity_id`) impossible à déclarer.
- Types ou collations différents entre FK et parent (`INT` vers `BIGINT UNSIGNED`, `utf8mb4_general_ci` vers
  `utf8mb4_bin`) : égalité différente entre source et destination.
- Clause `WHERE` relative au temps (à figer pour la reproductibilité) ; `WHERE` avec sous-requête, guillemets,
  noms réservés.
- Subset par FK désactivé (`IsNotForeignKeySafeSubset`).
- **`WHERE` avec `OR` de premier niveau** : seul sous pagination, et combiné à une seconde racine (fuite).
- **Deux racines de subset** sur le même graphe (intersection attendue) ; parent filtré par deux racines alors
  que l'enfant ne le rejoint que par une.
- Table rattachée au subset **uniquement par une FK virtuelle**.
- Ligne **`id = 0` sur une colonne `AUTO_INCREMENT`** (renumérotée sans `NO_AUTO_VALUE_ON_ZERO`), souvent la cible
  de la sentinelle `id_parent = 0`.
- **Trigger en destination qui écrit dans une table elle aussi synchronisée.**
- `ON DELETE CASCADE` / `SET NULL` en destination, interaction avec `truncateBeforeInsert`.

### Transformers (P1)

- Clé primaire transformée référencée par des FK (propagation), clé composite transformée.
- FK transformée alors que le parent ne l'est pas (intégrité cassée par la configuration).
- Colonne de tri ou de subset transformée (la reprise et le filtre doivent utiliser la valeur source).
- JavaScript : état partagé entre colonnes et **entre lignes** (fuite de l'état de la ligne précédente quand la
  ligne courante n'en pose pas) ; `input` ; exception sur certaines lignes ; retour `undefined`, objet dans une
  colonne entière, type incompatible.
- Transformer qui produit des collisions sur une contrainte unique (constante, dictionnaire trop petit à l'échelle,
  collation insensible à la casse).
- Sortie plus longue que la colonne (troncature ou erreur en mode strict).
- `Null` sur une colonne `NOT NULL`, `Default` sur une colonne sans défaut, transformer sur colonne générée.
- La chaîne `'null'` ou `'DEFAULT'` comme **vraie valeur** source (confusion avec les marqueurs de Benthos).
- Aucune donnée personnelle source recopiée en clair : contrôle systématique sur les colonnes transformées.

### Types et valeurs (P2)

- MySQL : `BIGINT UNSIGNED` max, `TINYINT(1)`, `BIT(1)`/`BIT(64)`, `YEAR`, `TIME` négatif ou > 24 h,
  `DATETIME(6)`, dates zéro `0000-00-00` selon `sql_mode`, `TIMESTAMP` et fuseau de connexion, `DECIMAL(65,30)`,
  `FLOAT`/`DOUBLE`, `CHAR` complété d'espaces, `ENUM` avec valeur vide, `SET`, JSON (unicode, grands nombres,
  imbrication), `GEOMETRY`/`POINT` (et index spatial), `BINARY(16)`, `VARBINARY` non UTF-8, `LONGBLOB` de plusieurs
  Mo (`max_allowed_packet` × taille de lot), emojis en `utf8mb4`, colonnes `latin1`, octet NUL dans du texte.
- Colonnes générées `VIRTUAL`/`STORED`, colonnes invisibles, `DEFAULT` par expression, `ON UPDATE
  CURRENT_TIMESTAMP` (la destination réécrit la valeur), contraintes `CHECK`.
- Identifiants : mots réservés (`order`, `group`, `key`), espaces, tirets, accents, casse mixte, 64 caractères,
  même nom de table dans deux schémas, colonne nommée comme un alias généré (`t_…`).
- PostgreSQL (phase 3) : tableaux, types enum et domaines, `GENERATED ALWAYS AS IDENTITY`, séquences, `jsonb`,
  `bytea`, `money`, `tsvector`, contraintes `DEFERRABLE`.
- SQL Server (phase 3) : `IDENTITY`, `rowversion`, colonnes calculées, `hierarchyid`, `xml`, `datetimeoffset`.

- FK auto-référencée `NOT NULL` (ligne racine qui se référence elle-même) ; `WHERE` avec `IS NULL`, `BETWEEN` ou une
  fonction, sous jointure (colonne ambiguë) ; colonnes `latin1` contenant de l'UTF-8 (mojibake) ;
  `lower_case_table_names` différent entre source et destination ; tables partitionnées ;
  `sql_generate_invisible_primary_key`. `BIT(64)` et `GEOMETRY` à travers le normaliseur d'Athanor sont à traiter
  en P1 (corruption silencieuse possible).

### Destination et configuration du job (P2)

- Destination au schéma différent (colonne en plus `NOT NULL` sans défaut, ordre des colonnes différent,
  `initTableSchema` désactivé), triggers en destination, vues dans le schéma source.
- Mappings vers des colonnes disparues de la source, nouvelles colonnes non mappées.
- `onConflict` do nothing / update, avec clé unique autre que la clé primaire.
- Plusieurs destinations ; source et destination de SGBD différents.

### Exploitation (P3)

- Perte de connexion ; délai de requête source. (Worker tué en cours de page : remonté en P1.)
- `sql_mode` ou fuseau différents entre source et destination.
- Échelle : 1 million de lignes, table avec 20 FK nullables filtrées, sous-requêtes de subset à plusieurs jointures.

## Outil

Structure validée le 2026-09-17, dans `bench/` (versionné, module Go du dépôt).

- **Un job par cas et par moteur** : un cas qui fait échouer le run ne masque pas les autres. L'attendu d'un cas
  peut être « le run échoue avec tel message ».
- **Attendu stocké hors du job**, dans un schéma `bench_oracle` de la source : `expected_rows(case_id, table,
  row_key, verdict, null_columns)` et `expected_columns(case_id, table, column, rule)` (règles : `unchanged`, `null`,
  `not_in_source_set`, `unique`, `max_len`, `stable_across_runs`, `matches_parent`). Table sans clé : `row_key` est une
  empreinte du contenu.
- `make bench/correctness` : petit volume, `MAX_TABLE_SYNC_PAGE_LIMIT=100` sur le worker, jobs en parallèle.
  `make bench/perf` : schéma combiné à l'échelle, worker à sa taille de page normale, 5 tours par moteur en
  alternance, worker redémarré avant chaque run et destination vidée. `make bench` enchaîne correction et
  perf.

  Le mode perf (`bench/perf`, commande `enginebench perf [-scale n] [-rounds n] [-skip-load]`) mesure un job
  seul sur la machine. Jeu de données : une table copiée entière, un subset sur deux niveaux de clés
  obligatoires, une clé nullable hors du chemin du subset, une table sans clé, des lignes larges (JSON, texte,
  décimaux, binaire, 20 colonnes de remplissage), et une clé transformée que les filles suivent par Redis.
  L'échelle multiplie une unité de 31 000 lignes ; 100 donne les ~3,1 millions de lignes de la comparaison.
  Le rapport donne la durée médiane des tours et son étendue, les lignes écrites par seconde, ce que le run
  ajoute à la mémoire du conteneur (mesurée par échantillonnage, la référence étant prise juste avant le run :
  le conteneur porte la chaîne Go et le rechargeur, dont la mémoire écrase celle du run), la répartition du
  temps par activité, et **les lignes écrites par table pour chaque moteur** — c'est ce dernier tableau qui
  dit que les deux moteurs ont fait le même travail.
- Dossiers : `cmd/enginebench` (CLI `list | run | verify`), `schema` (modèle neutre et DDL par SGBD), `cases` (un
  fichier par famille), `gen` (chargement de la source depuis la graine de chaque cas), `oracle` (attendu), `env`
  (serveurs), `orchestrate` (API Connect), `verify`, `report`, `baseline.json` (écarts connus).
- `compose.bench.yml` se pose sur `compose.dev.yml` : trois MySQL 8.0 jetables en `tmpfs` (source `3309`,
  destination Benthos `3310`, destination Athanor `3311`), et le worker recréé avec la taille de page du banc.
  Les destinations du premier test réel (`3307`, `3308`) ne sont pas touchées. `make bench/up`, `make bench/down`.
- Le banc lit la taille de page dans le plan stocké du run et s'arrête si elle diffère de la sienne : sans ce
  contrôle, les cas de pagination passeraient sans être exercés.
- Valeurs comparées dans le texte que la base imprime (hexadécimal pour le binaire), lu de la même façon dans la
  source et la destination : aucun type Go n'arrondit une valeur entre les deux.
- Rapport : `bench/out/<date>-<commit>/report.json` et `report.md` ; code de sortie non nul si un cas régresse par
  rapport à `baseline.json`. Critère d'acceptation des corrections : la baseline se vide.

## Passage du banc (2026-09-17, commit `7691bda2` + banc, page de 100 lignes)

50 cas MySQL, un job par cas et par moteur, verdicts identiques sur deux passages complets. La source est en
`super_read_only` pendant tous les runs (scénario « source sur un réplica ») : aucun moteur n'y écrit.
`retry-keyless-table-duplicates` demande des pages de 2 500 lignes (`make bench/large-pages`).

| Priorité | Moteur | OK | Écart | Échec du run | Run sans fin | Échec attendu absent |
|---|---|---|---|---|---|---|
| P1 | Benthos | 24 | 8 | 4 | 5 | 0 |
| P1 | Athanor | 22 | 14 | 4 | 0 | 1 |
| P2 | Benthos | 4 | 1 | 1 | 3 | 0 |
| P2 | Athanor | 4 | 0 | 4 | 0 | 1 |

Constats principaux :

- **Benthos ne fait pas échouer un run sur une erreur d'écriture non classée : il réessaie jusqu'au délai de
  l'activité (10 min).** Vu sur valeur hors bornes, troncature, `NOT NULL`, colonne générée, droit `INSERT` refusé,
  colonne ambiguë, clé binaire dans le jeton. Le banc arrête ces runs après 3 min (« run sans fin »).
- **Pertes silencieuses communes** (run réussi) : page finissant sur `NULL` (150 lignes sur 250), lignes identiques
  à cheval sur deux pages (5 sur 155), FK nullable sur le chemin du subset (10 sur 20), orphelins de FK virtuelle,
  trigger de destination qui écrit dans une table synchronisée (10 lignes en trop).
- **Corruptions silencieuses propres à Benthos** : microsecondes de `DATETIME(6)` et `TIMESTAMP(6)` mises à zéro,
  grands nombres JSON arrondis, `ON UPDATE CURRENT_TIMESTAMP` réécrit par la passe de mise à jour, lignes à FK
  nullable orpheline dans la source écartées au lieu d'être gardées à `NULL`.
- **Propres à Athanor** : orphelins écrits sur tous les cas de FK hors chemin (FK suspendues, intégrité à
  implémenter) ; `OR` avec deux racines : 10 lignes hors subset (Benthos est sauvé par le refus de la FK) ;
  nouvelle tentative en `INSERT IGNORE` : troncature acceptée et run réussi ; table sans clé : 2 000 lignes en
  double après un échec partiel de page ; colonnes générées : échec du run même en valeur par défaut.
- **Jeton de reprise** : `BIGINT UNSIGNED` au-delà de 2^53 casse les deux moteurs ; `DATETIME(6)` casse Benthos ;
  `BINARY(16)` casse les deux ; `DECIMAL`, clé composite et collation insensible à la casse passent.
- `OR` de premier niveau sous pagination : `Duplicate entry` dès la page 2 sur les deux moteurs. `id = 0` en
  `AUTO_INCREMENT` : renuméroté, `Duplicate entry` sur les deux. FK auto-référencée `NOT NULL` : refusée comme
  dépendance circulaire. Destination sans droit d'écriture : aucun contrôle au démarrage, Athanor échoue au premier
  `INSERT`, Benthos réessaie sans fin.
- Durées : Benthos met 5 à 22 s par cas (16 à 22 s avec subset par FK), Athanor 0,4 à 1,7 s. Cause non analysée.

| Cas | Priorité | Benthos | Athanor |
|---|---|---|---|
| `destination-trigger-writes-synced-table` | P1 | écart | écart |
| `fk-composite-partially-null` | P1 | OK | écart |
| `fk-cycle-two-tables` | P1 | OK | écart |
| `fk-diamond` | P1 | OK | écart |
| `fk-nullable-on-subset-path` | P1 | écart | écart |
| `fk-self-reference-nullable` | P1 | OK | écart |
| `fk-several-to-same-parent` | P1 | OK | écart |
| `fk-source-orphans` | P1 | écart | écart |
| `fk-virtual` | P1 | écart | écart |
| `fk-virtual-only-path` | P1 | OK | OK |
| `page-binary16-key` | P1 | run sans fin | échec du run |
| `page-case-insensitive-key` | P1 | OK | OK |
| `page-composite-key` | P1 | OK | OK |
| `page-datetime6-key` | P1 | échec du run | OK |
| `page-decimal-key` | P1 | OK | OK |
| `page-duplicate-rows` | P1 | écart | écart |
| `page-empty-and-single-row` | P1 | OK | OK |
| `page-exact-multiple` | P1 | OK | OK |
| `page-null-order-values` | P1 | écart | écart |
| `page-unsigned-bigint-key` | P1 | échec du run | échec du run |
| `retry-insert-ignore-masks-truncation` | P1 | run sans fin | échec attendu absent |
| `retry-keyless-table-duplicates` | P1 | OK | écart |
| `subset-parent-filtered-twice` | P1 | OK | écart |
| `subset-two-roots` | P1 | OK | OK |
| `tr-constant-on-unique-column` | P1 | OK | OK |
| `tr-generate-personal-data` | P1 | OK | OK |
| `tr-null-on-not-null-column` | P1 | run sans fin | OK |
| `tr-null-on-nullable-column` | P1 | OK | OK |
| `tr-order-column-transformed` | P1 | OK | OK |
| `tr-output-longer-than-column` | P1 | run sans fin | OK |
| `types-auto-increment-zero` | P1 | échec du run | échec du run |
| `types-binary` | P1 | OK | OK |
| `types-decimal-float` | P1 | OK | OK |
| `types-enum-set-bit` | P1 | OK | OK |
| `types-geometry` | P1 | OK | OK |
| `types-integers` | P1 | run sans fin | OK |
| `types-json` | P1 | écart | OK |
| `types-temporal` | P1 | écart | OK |
| `types-text` | P1 | OK | OK |
| `where-or-paginated` | P1 | échec du run | échec du run |
| `where-or-two-roots` | P1 | OK | écart |
| `columns-generated-default` | P2 | OK | échec du run |
| `columns-generated-passthrough` | P2 | run sans fin | échec du run |
| `columns-invisible` | P2 | OK | OK |
| `columns-on-update-timestamp` | P2 | écart | OK |
| `fk-self-reference-not-null` | P2 | échec du run | échec du run |
| `identifiers-quoting` | P2 | OK | OK |
| `rights-destination-read-only-account` | P2 | run sans fin | échec attendu absent |
| `table-partitioned` | P2 | OK | OK |
| `where-unqualified-under-join` | P2 | run sans fin | échec du run |

## État au 2026-09-17 au soir (58 cas)

Corrigé dans le calcul partagé, donc pour les deux moteurs : parenthèses autour du `WHERE` de l'utilisateur ;
jeton de reprise typé (entiers au-delà de 2^53, binaire, dates) ; pagination seulement sur une clé sans colonne
nullable, table sans clé lue en un seul flux ; FK nullable sur le chemin du subset en `LEFT JOIN` ; toutes les
colonnes du `WHERE` MySQL qualifiées ; « do nothing » MySQL sans `INSERT IGNORE` ; **étape 1 de l'intégrité** (FK
nullable lue à `NULL` quand la ligne parente n'est pas sélectionnée) ; **étape 3** (contrôle des orphelins en fin de
run, réparation seulement si le run a vidé la destination) ; **contrôle des droits au démarrage du run** (MySQL).

Dans Benthos : erreurs MySQL permanentes déclarées critiques (plus de run qui réessaie dix minutes), fractions de
seconde conservées. Dans Athanor : une transaction par page ; **étape 2 de l'intégrité** (parents obligatoires
vérifiés à l'écriture, valeur sentinelle respectée) ; colonnes générées laissées à la destination ; `id = 0` et
`0000-00-00` écrits tels quels ; **clés transformées suivies par les FK via Redis**.

| Priorité | Moteur | OK | Écart | Échec du run | Échec attendu absent |
|---|---|---|---|---|---|
| P1 | Athanor | 44 | 2 | 0 | 0 |
| P1 | Benthos | 38 | 5 | 2 | 1 |
| P2 | Athanor | 11 | 0 | 1 | 0 |
| P2 | Benthos | 9 | 1 | 2 | 0 |

Cas encore hors attendu :

| Cas | Priorité | Benthos | Athanor |
|---|---|---|---|
| `destination-trigger-writes-synced-table` | P1 | écart | écart |
| `fk-source-orphans` | P1 | écart | OK |
| `fk-source-orphans-destination-kept` | P1 | échec attendu absent | OK |
| `retry-keyless-table-duplicates` | P1 | écart | OK |
| `types-auto-increment-zero` | P1 | échec du run | OK |
| `types-integers` | P1 | échec du run | OK |
| `types-json` | P1 | écart | OK |
| `types-zero-dates` | P1 | écart | écart |
| `columns-generated-passthrough` | P2 | échec du run | OK |
| `columns-on-update-timestamp` | P2 | écart | OK |
| `fk-self-reference-not-null` | P2 | échec du run | échec du run |

Restent hors attendu sur Athanor : le trigger de destination qui écrit dans une table synchronisée (MySQL ne sait
pas suspendre un trigger : à signaler au pré-vol, ou à retirer puis recréer), la FK auto-référencée `NOT NULL`
(refusée comme dépendance circulaire par le calcul des passes, alors qu'Athanor écrit en une passe), les dates à
mois ou jour nul (`2024-00-00`, converties par le pilote dès la lecture : `parseTime`).

Limites connues de ce qui a été ajouté : la lecture des privilèges suppose un compte `'user'@'%'` (un compte sur
hôte précis ou par rôle est « inconnu » et ne bloque pas) ; une clé auto-référencée vers une clé transformée est
refusée par Athanor ; PostgreSQL et SQL Server ne sont pas encore couverts par le contrôle des parents (types des
paramètres) ni par le contrôle des droits.

Constats d'exploitation : le worker garde les connexions d'un run ouvertes une minute ou plus après sa fin ; un
passage complet du banc dépasse les 151 connexions par défaut de MySQL (serveurs du banc portés à 1 000). Observé
une fois sur cinq, sous charge : un run Benthos terminé « réussi » avec une table vide après une erreur
d'écriture (`types-integers`), course dans l'arrêt du flux sur erreur.

Restent à écrire : JavaScript à état partagé, destination de SGBD ou de schéma très différent, worker réellement
tué en cours de page, `lower_case_table_names`, `sql_generate_invisible_primary_key`, mode `bench/perf`.

## Reprise du 2026-09-17 (soir) : la durée mesurée jusqu'ici était un minuteur

Le passage complet sortait en code 0 sur 58 cas (Athanor 54 OK, Benthos 47). Trois constats
et quatre corrections.

**L'écart de durée entre les moteurs n'était pas un écart de moteur.** Mesuré cas par cas,
run isolé : `fk-diamond` (3 tables, 52 lignes) prenait 15,9 s avec Benthos et 1,2 s avec
Athanor. Les journaux montrent cinq secondes exactement entre la dernière ligne lue et
l'écriture du dernier lot. Cause : la police de lot d'une destination valait `count: 100,
period: 5s` (`getParsedBatchingConfig`, défaut hérité de Neosync). Le batcher de benthos vide
ce qu'il détient quand son entrée se ferme, mais l'entrée ne se ferme qu'une fois ses lignes
acquittées, et les lignes d'un lot partiel sont justement celles qui attendent
(`component/input/async_reader.go`) : seule la période les libérait, une période par page.
L'arithmétique collait sur tout le banc (1 table → 5,9 s ; 3 → 15,9 s ; 4 → 21,6 s) et sur le
test de recette (66 tables → 2 min 56 s).

Corrigé en marquant, dans notre entrée SQL, la dernière ligne d'une page (lecture avec une
ligne d'avance) et en faisant vider le lot sur ce marqueur (`check` de la police de lot). La
période reste : elle libère les lignes que le marqueur n'atteint pas, et la sortie `error`
la garde pour arrêter l'activité sans attendre la fin de la page. Retirer la période sans le
marqueur bloque le flux jusqu'au délai de l'activité — essayé, vérifié.

Effet, verdicts inchangés sur les 58 cas : cumul Benthos 562 s → 185 s.

**Les durées du banc de correction ne sont pas des mesures.** Sur les mêmes 10 cas et le même
code, en ne changeant que le parallélisme : Athanor 12,9 s cumulé (médiane 1 252 ms) à
`-parallel 1` contre 19,0 s (1 857 ms) à `-parallel 6` ; Benthos 12,2 s (1 248 ms) contre
24,6 s (2 071 ms). À cette échelle les deux moteurs sont indiscernables : tout est coût fixe
d'orchestration. Seul le mode `perf` (runs isolés, worker redémarré) donnera des chiffres.

**Deux pertes silencieuses corrigées dans Athanor.**

- Une session dont les réglages n'ont pas pu être rétablis (`SET FOREIGN_KEY_CHECKS=1`,
  `sql_mode`) retournait au pool telle quelle, un `Rollback` ne défaisant pas un réglage de
  session : la table suivante écrite sur cette connexion l'était clés étrangères coupées.
  La connexion est maintenant tuée plutôt que rendue.
- Le publieur de clés transformées publiait la nouvelle clé de **toutes** les lignes du lot,
  y compris celles que les écrivains sous lui venaient d'écarter (parent obligatoire absent,
  parent non copié) : une table fille traduisait alors sa clé vers une ligne que la
  destination n'a jamais reçue. Les écrivains disent désormais quelles lignes ils écartent,
  et le publieur les retire de ce qu'il publie.

**Défaut restant, commun aux deux moteurs, découvert par le cas `tr-key-of-discarded-row` :**
la passe d'insertion d'une table à subset laisse ses FK nullables à une passe de mise à jour
(`buildConstraintHandlingConfigs`), donc rien ne la fait attendre la table qui publie la
clé. Athanor écrit cette colonne dans sa passe unique, avant que la clé existe, et la met à
`NULL` ; Benthos échoue sur la lecture Redis (`redis: nil`). Les deux verdicts sont enregistrés
comme écarts connus. Choix à faire : rendre la dépendance au parent au calcul partagé quand
la clé du parent est transformée (profite aux deux moteurs, mais peut recréer un cycle), ou
faire exécuter les passes de mise à jour à Athanor.

Deux cas ajoutés : `tr-key-of-discarded-row` (P1, hors attendu sur les deux moteurs) et
`fk-parent-key-collation` (P1, OK sur les deux : le contrôle des parents compare bien avec la
collation de la colonne, pas celle de la connexion — garde-fou pour PostgreSQL et SQL Server).

## Test de recette rejoué (2026-09-17 au soir, job `demo-outil-franchise`, 66 tables)

Mesuré sur la même source, le même soir, worker à sa taille de page normale (100 000).

| Moteur | Commit `14c7a56a` (début de session) | Avec les corrections de la session |
|---|---|---|
| Benthos | 461 s | **361 s** |
| Athanor | 48 s | **42 s** |

- **Contenu : 66 tables identiques sur 66** entre les deux destinations (empreinte `crc32` par ligne sur
  toutes les colonnes). La seule colonne qui diffère est `REF_STATION_MONTAGE.DT_DERNIER_IMPORT`, une date que
  l'application de recette met à jour entre les deux runs — écart de source, pas de moteur. La comparaison
  précédente donnait 55 tables sur 66.
- **Zéro orphelin** dans les deux destinations, sur toutes les clés étrangères du schéma. Le premier test réel
  en comptait 36 côté Athanor.
- Le plancher de 5 s par table a disparu : durée médiane d'une sync de table passée de **5 s à 0,2 s**
  (73 syncs sous 5 s sur 90, contre 10 sur 66 auparavant), et **aucun** lot vidé par période sur tout le run.
- Le mur de Benthos reste dominé par trois tables (211 s, 71 s, 70 s) avec un plafond de parallélisme à 3.
- Attribution vérifiée : rejouer sans le seul changement d'ordre des clés donne 383 s (contre 361 s avec) —
  ce changement ne coûte rien. La référence de 176 s du premier test réel n'est pas comparable : elle précède
  le travail d'exactitude de la session précédente (filtres `EXISTS` sur les FK nullables, contrôle
  d'intégrité de fin de run, contrôle des droits), qui coûte du temps et que les 461 s incluent.
- `main` ne peut pas exécuter ce job : il échoue en 84 s sur `ReferenceError: neosync is not defined`
  (transformers JavaScript, corrigé par la branche).

## Comparaison mesurée (2026-09-18, commit `f648a7c6`, échelle 100)

Schéma combiné, 3 100 000 lignes en source, 1 725 000 écrites après subset. Un job seul sur la machine,
worker redémarré avant chaque run, destination vidée, 5 tours par moteur en alternance.

| Moteur | Durée médiane | min | max | Lignes/s | Mémoire du run |
|---|---|---|---|---|---|
| Benthos | 159,5 s | 140,8 s | 174,0 s | 10 813 | 143 Mio |
| **Athanor** | **24,8 s** | 24,6 s | 27,9 s | **69 514** | **50 Mio** |

**Athanor est 6,4 fois plus rapide, prend 2,9 fois moins de mémoire et varie dix fois moins** (étendue de
3,3 s contre 33,2 s). Les deux moteurs écrivent exactement les mêmes lignes, table par table : c'est le
tableau du rapport qui l'établit.

Le temps est entièrement dans les syncs de table : cumul de 301,1 s pour Benthos contre 33,9 s pour Athanor,
le reste (génération des configs, init de schéma, contrôle des droits, contrôle d'intégrité de fin de run)
étant identique et négligeable pour les deux — le contrôle d'intégrité coûte 1,2 s à Benthos et 1,4 s à
Athanor, soit 6 % du run d'Athanor.

**La taille des lots n'explique pas l'écart.** Benthos écrit par 100 lignes et Athanor par 1 000 : à parité
(`-batch-count 1000`, 3 tours), Benthos met 156,3 à 169,5 s, soit rien de gagné, et double la mémoire qu'il
tient (254 Mio). Son coût est ce qu'il fait de chaque ligne, une par une dans le flux, pas la façon dont il
les écrit.

Limites à énoncer avec ces chiffres : tout tourne sur une seule machine (source, deux destinations, worker,
Temporal, API se disputent le processeur), MySQL seul, source et destination homogènes, une seule destination.

## État au 2026-09-18 : Athanor passe les 59 cas

Les trois écarts MySQL restants ont été fermés.

- **Dates zéro** : les connexions lisent désormais les dates MySQL comme le serveur les
  imprime (`parseTime` coupé). Une date que MySQL accepte et que Go ne sait pas porter
  revenait déplacée en silence : `2024-00-00` lu comme le dernier jour de novembre 2023.
  Athanor passe le cas ; Benthos échoue franchement le run, faute de relâcher le `sql_mode`
  de sa destination — une erreur franche valant mieux qu'une valeur changée.
- **Clé auto-référencée `NOT NULL`** : une table ne s'attend plus elle-même. La dépendance
  était vide de sens (les lignes qui se référencent sont dans la même passe) et faisait
  refuser **le job entier**, avant toute lecture, dès qu'une table héritée en portait une.
  Athanor écrit la table en une passe et garde tout ; Benthos atteint l'écriture et échoue
  sur la contrainte, ce qui est la limite d'une écriture ligne à ligne clés actives.
- **Trigger de destination** : les triggers portés par les tables du job sont retirés avant
  les syncs et recréés après le contrôle d'intégrité, depuis la définition que la
  destination donne elle-même. Ce qui les recrée est enregistré dans le run context avant
  le retrait et journalisé en avertissement : un run terminé entre les deux laisse dans son
  historique de quoi les rétablir. PostgreSQL et SQL Server ne sont pas concernés : tous
  deux savent désactiver un trigger sans le supprimer.

| Priorité | Moteur | OK | Écart | Échec du run |
|---|---|---|---|---|
| P1 | **Athanor** | **47** | 0 | 0 |
| P1 | Benthos | 40 | 3 | 3 |
| P2 | **Athanor** | **12** | 0 | 0 |
| P2 | Benthos | 9 | 1 | 2 |

`retry-keyless-table-duplicates`, que le passage standard n'exerce pas (il demande des pages
de 2 500 lignes), passe aussi sur les deux moteurs : **Athanor est à 59 cas sur 59**.

## Portage PostgreSQL (2026-09-18)

### Le banc adresse un second SGBD

- Les verdicts sont enregistrés **par SGBD** (`baseline.json` a un premier niveau `mysql` /
  `postgres`) : sans ça un résultat PostgreSQL écraserait un écart connu MySQL, ou lui serait comparé.
- Le rendu répond de tout ce que le banc écrit en SQL : DDL, tables de l'oracle, session de chargement,
  paramètres liés, lecture des lignes, bascule en lecture seule, comptes restreints. Un cas vit dans un
  **conteneur** : une base sur MySQL, un schéma sur PostgreSQL — ce qu'un mapping de job appelle un schéma
  dans les deux (`Case.Schema()`).
- Les lignes sont lues par une expression que le rendu donne, pour que le serveur imprime lui-même chaque
  valeur : PostgreSQL convertit (`CAST … AS text`), MySQL imprime par défaut, et les deux écrivent les
  octets bruts `x'<hex>'`. Aucun type Go n'arrondit une valeur en chemin.
- Un cas ne nomme ses SGBD que lorsqu'il porte sur ce qu'une seule base a — un type, un réglage, une
  syntaxe — jamais parce qu'il y a été écrit d'abord. **20 cas appartiennent à MySQL, 40 sont neutres**, et
  un test demande à chaque rendu de les rendre : un cas qui ment sur son appartenance échoue avant tout
  serveur.
- Trois PostgreSQL 16 jetables (`make bench/pg-up`, ports 3312/3313/3314), `BENCH_DIALECT=postgres`.
- Source figée : PostgreSQL n'a pas d'interrupteur global, `default_transaction_read_only` sur la base sert
  d'équivalent. **Piège** : lever le réglage est une écriture, qu'une session qui en a hérité ne peut plus
  faire ; elle en sort d'abord pour elle-même. Et les connexions déjà oisives dans le pool gardent l'ancien
  réglage : le pool oisif est vidé après chaque bascule.

### Corrections du produit que PostgreSQL a rendues visibles

- **Contrôle des parents** : paramètres nus dans le `UNION ALL`, `operator does not exist: bigint = text`.
  La première branche de la table dérivée ne lit rien et sert à donner à chaque colonne **le type et la
  collation de la clé parente** — aucun nom de type à transporter, aucun schéma de destination à deviner,
  et le planificateur supprime la branche (vérifié par `EXPLAIN`). `fk-parent-key-collation` passe sur les
  deux SGBD.
- **Borne de paramètres par dialecte** : 500 clés fixes dépassaient les 2 100 paramètres de SQL Server dès
  qu'une clé avait trois colonnes. Défaut latent, corrigé avant d'atteindre SQL Server.
- **Suspension des FK** : `SET LOCAL session_replication_role = replica`, porté par la transaction de la
  page. Rien ne reste sur la connexion, rien à rétablir. Exige un superutilisateur ou
  `GRANT SET ON PARAMETER` (PG 15+) — les deux voies vérifiées.
- **Qualification du `WHERE` PostgreSQL** (calcul partagé, les deux moteurs) : le qualificateur énumérait
  les formes d'expression et laissait nues les colonnes sous `IS NULL`, `BETWEEN`, `IN`, un appel de
  fonction, un `CASE`. Ambiguës dès que la table est jointe à une fille qui porte les mêmes. La clause est
  parcourue entièrement, comme côté MySQL.
- **Jeton de reprise** : pgx rend `*pgtype.Timestamp`, que le jeton typé refusait — run arrêté net à la fin
  de sa première page. Un enveloppeur qui sait dire la valeur qu'il porte voyage désormais comme elle.

### Passage PostgreSQL (40 cas)

| Priorité | Moteur | OK | Écart | Échec du run | Échec attendu absent |
|---|---|---|---|---|---|
| P1 | **Athanor** | **34** | 0 | 0 | 0 |
| P1 | Benthos | 31 | 2 | 0 | 1 |
| P2 | **Athanor** | **4** | 0 | 1 | 0 |
| P2 | Benthos | 3 | 0 | 2 | 0 |

Le seul échec d'Athanor est `on-conflict-update-unique-key`, **partagé avec Benthos** : `ON CONFLICT` ne
résout que la cible qu'il nomme, là où MySQL se déclenche sur n'importe quelle clé unique. Les écarts de
Benthos sont ses limites propres, les mêmes que sur MySQL.

### Suite du portage (2026-09-18, après midi)

**Famille de cas de types PostgreSQL** (8 cas, l'équivalent des `types-*` de MySQL) : `numeric` sans borne et
`NaN`, `real`, `money`, horodatages à la microseconde, dates avant notre ère, `infinity`, `interval`, texte
(sans octet NUL, que PostgreSQL refuse), `bytea`, `json` contre `jsonb`, tableaux, `uuid`, `inet`, `cidr`,
`macaddr`, type énuméré et domaine déclarés dans le schéma du cas (`SchemaSetup`, joué avant les tables).
Chaque valeur est écrite comme le serveur l'imprime — demandé au serveur, pas supposé. pgx lie une chaîne à
tous ces types sans conversion.

- **Corruption silencieuse P1 dans Benthos** : une colonne `json` (que PostgreSQL garde mot pour mot) revient
  réordonnée et sa clé en double supprimée ; un nombre `jsonb` au-delà d'un float64 revient arrondi
  (`12345678901234567890` → `12345678901234567000`). Le même défaut que sur MySQL. Athanor passe les huit.

**Identités** : PostgreSQL refuse une valeur fournie dans une colonne `GENERATED ALWAYS AS IDENTITY` sans
`OVERRIDING SYSTEM VALUE`. La clause est acceptée sur n'importe quelle table, avec ou sans identité : Athanor
la dit à chaque insertion PostgreSQL, **sans rien ajouter au plan neutre** (ce que j'avais d'abord proposé).
`types-pg-identity` sème des identifiants qu'une numérotation par la destination ne pourrait pas atteindre.

**FK composite lue dans le mauvais ordre — perte silencieuse P1, commune aux deux moteurs, propre à
PostgreSQL.** La requête des FK ordonnait les colonnes par leur position dans la table (`attnum`) et agrégeait
les colonnes référencées sans ordre : `FOREIGN KEY (b, a) REFERENCES p (x, y)` revenait appariée `a↔x, b↔y`.
Jointure du subset, contrôle des parents et contrôle d'intégrité de fin de run (qui, destination vidée et
violations ignorées, **supprime** les lignes qu'il croit orphelines) lisaient tous la clé de travers. Corrigé en
suivant la position dans `conkey`/`confkey`. `fk-composite-column-order` : 9 et 11 lignes fausses avant, OK
après ; MySQL lisait déjà ses clés dans l'ordre de la contrainte.

**`on-conflict-update-unique-key` : tranché pour l'échec explicite.** `ON CONFLICT DO UPDATE` ne résout que
la cible qu'il nomme ; un conflit sur une autre clé unique n'a pas de réponse qu'une copie puisse donner sans
deviner quelle ligne garder. Le cas exige désormais l'échec, dans les mots de PostgreSQL.

**Contrôle des droits PostgreSQL au démarrage du run.** La lecture MySQL des privilèges accordés ne se
transpose pas : `information_schema.table_privileges` ne liste que ce qui est accordé nommément au compte, et
ignorerait un superutilisateur, le propriétaire ou un compte qui tient ses droits d'un rôle — des runs
légitimes auraient été bloqués. Le contrôle interroge `has_table_privilege` / `has_schema_privilege`.
Serveur en écriture : `transaction_read_only`, vrai sur un réplica comme sous
`default_transaction_read_only`. Suspension des FK : sondée avec l'instruction même qu'Athanor écrit, dans
une transaction aussitôt annulée, **seulement si le job tourne en Athanor** — la décision du moteur vit
désormais sur la politique, partagée par la synchro et le contrôle. Le banc sait dire qu'un seul moteur doit
échouer (`FailingEngines`) : sans le droit de suspendre les FK, Athanor s'arrête au démarrage avec le `GRANT`
à exécuter, et Benthos passe.

**Triggers de destination sur PostgreSQL.** Les deux moteurs ne copiaient pas les mêmes lignes : le rôle
`replica` d'Athanor fait taire les triggers ordinaires (Benthos écrivait 10 lignes en trop) mais réveille ceux
réglés `ENABLE REPLICA` (Athanor écrivait alors 10 lignes en trop, Benthos non). L'activité partagée les
désactive pour les deux moteurs et remet chacun dans **son état exact** (`O`, `R`, `A`), sans toucher un
trigger déjà désactivé. Le trigger n'est jamais supprimé ; l'enregistrement de restauration garde le nom de
champ d'avant, pour qu'un run suspendu par un ancien worker soit restauré.

**Corrections du plan, vérifiées sur un serveur** : le risque « `information_schema` masque les FK à un compte
sans droit » ne s'applique pas — Husonym lit les FK dans `pg_catalog`. Et un compte avec le seul droit
`SELECT` voit bien les colonnes des clés primaires et uniques dans `key_column_usage`.

### Reste à faire sur PostgreSQL

- `ALTER TABLE … DISABLE TRIGGER` demande d'être propriétaire de la table : un compte non propriétaire, sur
  une destination qui a des triggers, échouera à l'étape des triggers. À ajouter au contrôle des droits.
- Cas MySQL sans équivalent PostgreSQL écrit : collation insensible à la casse (collation ICU non
  déterministe), ordre des colonnes de destination, colonne `NOT NULL` en plus.
- Comparaison mesurée `bench/perf` sur PostgreSQL (le jeu de données n'a qu'un rendu MySQL).

## Reprise du 2026-09-18 : triggers de destination fiables

La relecture du travail de la veille a montré que la suspension des triggers abîmait les
destinations bien au-delà du « run arrêté entre le retrait et la remise » :

- **La remise n'avait lieu qu'en cas de succès.** Tout run en échec (une table, le contrôle
  d'intégrité, une annulation) sortait du workflow avant elle : triggers MySQL **supprimés** pour
  de bon (le run suivant ne les voyait plus), triggers PostgreSQL laissés désactivés.
- **L'enregistrement était écrit après la suspension**, contrairement à ce que disait son
  commentaire : un échec au deuxième trigger ou sur la deuxième destination ne laissait aucune
  trace des premiers.
- **MySQL recréait sans guillemets** (`wrapIdempotentTrigger`) : un trigger au nom à tiret était
  supprimé puis perdu, et le run échouait après avoir tout écrit. `EscapeMysqlColumn` ne doublait
  pas non plus les accents graves d'un nom.
- **MySQL recréait un autre trigger** : sans son `DEFINER` (il s'exécutait ensuite avec les droits
  du compte Husonym), sous le `sql_mode` et la collation de la session du worker, dans un ordre
  d'exécution quelconque (`ACTION_ORDER` lu sans tri). Même le cas existant revenait avec
  `utf8mb4_general_ci` au lieu de `utf8mb4_0900_ai_ci`.

Le banc compare désormais, **pour chaque cas**, les triggers de la destination avant et après le
run (définition, état, definer, `sql_mode`, collation, ordre) ; un écart compte comme tel, run
réussi ou en échec attendu. Quatre cas neutres et un propre à MySQL, tous hors attendu avant la
correction : run en échec après la suspension, run **arrêté de force** triggers retirés (le banc
sait maintenant arrêter un run sur une condition), nom à guillemets, definer / `sql_mode` /
collation / ordre.

Corrections :

- L'enregistrement est **rattaché au job** et écrit **avant** toute suspension, fusionné avec ce
  qu'une tentative ou un run précédent a laissé (ce qui y est déjà gagne : relu après la
  suspension, un trigger n'existe plus ou est désactivé). La remise ne garde dans
  l'enregistrement que ce qu'elle n'a pas pu remettre.
- Le workflow remet les triggers sur **toute sortie** qui suit la suspension, dans un contexte
  détaché (version 2 de `destination-triggers`). Un run arrêté de force, où aucun code ne
  s'exécute, laisse l'enregistrement au run suivant du job, qui remet tout à sa fin.
- MySQL recrée chaque trigger comme `information_schema.TRIGGERS` le décrit : `DEFINER`,
  `sql_mode` et collation de création, dans l'ordre où ils se déclenchent, sur une seule session
  dont les réglages sont rendus ensuite (la session est tuée si elle ne peut pas l'être).

Limites connues : deux runs du même job en même temps partagent l'enregistrement (le premier qui
finit remet les triggers pendant que l'autre écrit) ; le `character_set_client` d'un trigger
MySQL devient celui de la connexion du worker (les octets du corps sont envoyés dans ce jeu,
la collation qui décide des comparaisons est rétablie). Recréer un trigger dont le definer n'est
pas le compte du job demande `SET_USER_ID` (ou `SUPER`) : c'est au contrôle des droits de le dire
au démarrage (étape suivante).

## Contrôle des droits complet (2026-09-18)

Vérifié sur un serveur avant d'écrire quoi que ce soit :

- **MySQL, compte par rôle ou sur un hôte précis** : la lecture de `information_schema` (compte
  supposé `'user'@'%'`) ne voyait rien, le run n'était jamais arrêté. `SHOW GRANTS` (sans `FOR`)
  donne les droits du compte de la session, rôles actifs fusionnés ; `EXPLAIN` d'une instruction
  est refusé exactement comme elle le serait, sans l'exécuter.
- **MySQL cache les triggers d'une table à un compte sans `TRIGGER`** : 0 trigger vu, et le trigger
  se déclenche bien. Un tel compte ne suspendait rien et la copie recevait les lignes du trigger,
  run réussi : perte d'exactitude silencieuse, P1. Sans ce droit on ne peut pas savoir s'il y a des
  triggers : il est exigé sur toutes les tables de destination.
- Ni le vidage de la destination (`TRUNCATE` sur PostgreSQL, `DROP` sur MySQL), ni la suspension
  des triggers (propriétaire de la table sur PostgreSQL, qu'aucun droit ne remplace), ni la
  recréation d'un trigger MySQL dont le definer est un autre compte (`SET_USER_ID` ou `SUPER`)
  n'étaient contrôlés.

Ce qui change :

- MySQL : lecture, insertion, mise à jour et suppression sont demandées au serveur par `EXPLAIN`
  sur les colonnes que le run écrit, colonnes générées exclues. L'instruction n'est jamais
  exécutée, mais MySQL évalue les valeurs d'un `INSERT` dans une table partitionnée pour élaguer
  ses partitions : `DEFAULT` y est refusé sur une colonne sans défaut (trouvé par
  `table-partitioned`), `NULL` passe partout, partition par liste comprise ; l'`UPDATE` garde
  `DEFAULT`. `TRIGGER`, `DROP` et les droits dynamiques sont lus dans
  `SHOW GRANTS` (motifs de base `_`/`%`, révocations partielles, droits de colonne écartés).
  Serveur en lecture seule : `@@GLOBAL.read_only OR @@GLOBAL.super_read_only`, lisible par tout
  compte (l'ancienne lecture passait par `performance_schema`).
- PostgreSQL : `TRUNCATE` quand le job vide la destination ; `pg_has_role(propriétaire, 'USAGE')`
  pour chaque table qui porte un trigger actif.
- **Le contrôle a lieu après `GenerateBenthosConfigs`** (version 2 de `run-privilege-check`) et
  reçoit les tables et colonnes des configs. Par défaut, la génération retire en silence les
  mappings que la source n'a plus et peut en ajouter (passthrough des nouvelles colonnes) : des
  sondes construites sur les mappings auraient arrêté des runs légitimes. La génération ne lit que
  des métadonnées ; hooks, init de schéma et vidage viennent après le contrôle.
- Les échappements partagés `EscapeMysqlColumn` et `EscapePgColumn` sont corrigés (le second
  passait par le `%q` de Go : `"a\"b"`, illisible pour PostgreSQL) et remplacent les copies
  locales.

Cinq cas (quatre neutres, un propre à MySQL) : compte suffisant par rôle (garde le contrôle de
bloquer à tort), compte par rôle en lecture seule, destination à vider, triggers impossibles à
suspendre, definer étranger.

## Cas jumeaux PostgreSQL (2026-09-18)

Quatre cas écrits pour MySQL seul deviennent neutres, chaque SGBD avec sa préparation et ses
mots : colonne `NOT NULL` en plus en destination, ordre des colonnes différent (PostgreSQL ne sait
pas déplacer une colonne : la table de destination est recréée dans l'autre ordre), troncature
refusée à la première tentative et qui doit le rester à la reprise (`ALTER COLUMN … TYPE`), clé
texte sous collation insensible à la casse (collation ICU non déterministe déclarée dans le schéma
du cas, `SchemaSetupFor`). Tous passent sur les deux moteurs, pagination par curseur comprise.

**`retry-keyless-table-duplicates` n'exerçait plus rien.** Il provoquait son échec par un
trigger posé sur `JOURNAL`, table du job : depuis que le run retire les triggers de ses tables,
ce trigger était retiré avant l'écriture et aucune panne n'avait lieu — le cas passait à vide.
Refait sur PostgreSQL avec une contrainte `CHECK` dont la fonction échoue une seule fois (une
séquence s'en souvient : `nextval` n'est pas annulé avec l'instruction), et des lots de 10 lignes
pour que des lots soient validés avant l'échec. MySQL n'a aucun autre point d'accroche par ligne
(ni `CHECK`, ni colonne générée, ni défaut ne peuvent appeler une fonction utilisateur) ; ce que
le cas exerce, la reprise des moteurs, est le même sur les deux SGBD. **Benthos écrit 90 lignes
en double et termine le run « réussi »** (perte d'exactitude silencieuse, P1, propre à Benthos) ;
Athanor passe grâce à sa transaction par page. Le cas passe désormais dans le passage standard.

**Worker réellement tué en cours de page** (`worker-killed-mid-page`, neutre). Le banc insère en
destination, avant le run, une ligne de la deuxième page dans une transaction qu'il garde ouverte :
l'écriture de cette clé par le run attend, une partie de la page déjà écrite (lots de 10). Le banc
voit l'attente (`performance_schema.data_lock_waits`, `pg_locks`), tue le worker (`docker kill`),
puis annule sa ligne ; Temporal redonne la page au worker relancé une fois le battement de cœur
manqué (une minute). OK sur les deux moteurs et les deux SGBD : ni ligne perdue, ni ligne en
double. Un tel cas tourne seul, après les autres (tuer le worker ferait tomber leurs runs), et
dure environ 62 s par moteur.

Le banc compte 82 cas : 54 neutres, 16 propres à MySQL, 12 propres à PostgreSQL (83 avec le cas GIPK ajouté ensuite).

## Destination MySQL qui replie les noms en minuscules (2026-09-18)

`make bench/up-lower-case` recrée les deux destinations MySQL en `lower_case_table_names=1` (le
cas d'Azure Database for MySQL ou d'un serveur Windows), la source restant à 0 ; `make bench/up`
les rend. Le passage MySQL complet sert de balayage : les cas nomment leurs tables en majuscules.

Vérifié sur un serveur : sous `lower_case_table_names=1`, `information_schema` trouve une table
demandée dans n'importe quelle casse et **répond avec les noms repliés**, `SHOW GRANTS` aussi ; sous
0, il compare à la casse près. Premier passage : **les triggers de destination n'étaient ni trouvés
ni suspendus (10 lignes sans origine, run réussi)** et **le vidage avant écriture ne vidait rien**
(le run suivant retrouvait les lignes du précédent). Deux comparaisons en Go en étaient la cause :
`GetSchemaTableTriggers` refiltrait les réponses du serveur contre les noms demandés, et la
comparaison de schémas (par laquelle MySQL passe toujours : `shouldUseSchemaDrift`) cherchait les
tables de la destination sous l'orthographe de la source — aucune n'était « des deux côtés », d'où
zéro table vidée, et, avec `initTableSchema`, des différences fantômes qui auraient alimenté des
`ALTER`/`DROP`.

Corrigé à la racine : le `sqlmanager` MySQL **répond dans l'orthographe des noms qu'on lui a
demandés** (colonnes, contraintes, triggers, fonctions) ; sur un serveur qui compare exactement, la
réponse l'avait déjà. Le refiltrage des triggers, redondant, est retiré. Le contrôle des droits lit
`@@lower_case_table_names` et compare les noms de `SHOW GRANTS` comme le serveur. Le passage complet
sur destinations repliées sort en code 0.

Limite connue : le parent d'une clé étrangère situé dans un autre schéma que la table interrogée
garde l'orthographe du serveur (la lecture des contraintes ne connaît que les tables de son schéma).

## Clé primaire que le job ne lit pas (2026-09-18)

`table-generated-invisible-primary-key` (MySQL) : une table déclarée sans clé, créée sous
`sql_generate_invisible_primary_key=ON` ; le serveur lui ajoute une clé primaire invisible
`my_row_id` que les mappings du job ne nomment pas. **Benthos terminait le run « réussi » avec 0
ligne écrite sur 250** ; Athanor échouait franchement (« colonnes de tri absentes de la lecture »).
La cause, commune et sans rapport avec GIPK : le calcul partagé paginait sur la clé primaire même
quand le job ne la lit pas, la page suivante reprenant après des valeurs qu'aucune ligne lue ne
porte. Corrigé dans `getOrderByColumns` : seule une clé entièrement lue pagine (clé primaire,
contrainte ou index unique) ; sans elle, la table est lue en un seul flux, comme une table sans
clé. Lire la clé sans l'écrire, pour garder la pagination sur une grande table, reste possible ;
il faudrait d'abord vérifier que Benthos n'écrit que les colonnes mappées.

La création du schéma d'un cas passe désormais par une seule session jetable, pour qu'un réglage de
session posé par `SchemaSetupFor` s'applique aux tables créées ensuite.

## Comparaison mesurée sur les deux SGBD (2026-09-18, commit `38d82082`)

Échelle 100 : 3 100 000 lignes en source, 1 725 000 écrites, 5 tours par moteur en alternance,
worker redémarré avant chaque run. Les deux moteurs écrivent **exactement les mêmes lignes, table par
table, à chaque tour**.

| SGBD | Moteur | Durée médiane | min | max | Lignes/s | Mémoire du run |
|---|---|---|---|---|---|---|
| MySQL | Benthos | 162,5 s | 147,3 s | 169,3 s | 10 618 | 137 Mio |
| MySQL | **Athanor** | **25,1 s** | 22,8 s | 27,7 s | **68 657** | **54 Mio** |
| PostgreSQL | Benthos | 166,8 s | 164,2 s | 177,5 s | 10 339 | 140 Mio |
| PostgreSQL | **Athanor** | **32,8 s** | 32,6 s | 34,7 s | **52 590** | **89 Mio** |

Athanor est **6,5 fois** plus rapide que Benthos sur MySQL et **5,1 fois** sur PostgreSQL, avec 2,5
et 1,6 fois moins de mémoire. Sur PostgreSQL, Athanor écrit par `INSERT` groupés de 1 000 lignes :
`COPY` est la piste évidente pour réduire l'écart avec MySQL (non mesurée).

**Le portage PostgreSQL n'a rien coûté sur MySQL.** Une première série, à midi, donnait +20 % pour
Benthos et +15 % pour Athanor, tout dans les synchros de table. Mesure appariée, même après-midi,
3 tours chacun, source déjà chargée : `f648a7c6` (mesure du 17) 152,3 s / 23,6 s ; `3694fc8c`
(dates lues en texte, seul changement commun aux deux moteurs entre-temps) 146,3 s / 23,1 s ;
`38d82082` 146,6 s / 23,0 s. L'écart venait de la machine, qui porte aussi les clusters k3d
d'autres projets : **d'une heure à l'autre, la même mesure varie de 25 %**. Seule une comparaison
appariée, dans la même période, attribue un écart au code.

## Passes de mise à jour d'Athanor et test de recette (2026-09-18, soir)

**Décidé** : Athanor exécute une passe de mise à jour du plan **seulement** quand elle écrit une FK qui suit
une clé transformée ; les autres restent sans objet (FK suspendues, tout est écrit à l'insertion). Deux cas,
hors attendu sur Athanor avant la correction et OK sur Benthos : `tr-key-self-reference` (Athanor refusait la
table) et `tr-keys-in-cycle` (**136 références laissées à `NULL` en silence**). La passe d'insertion écrit ces
FK `NULL` ; la passe de mise à jour, qui attend la table parente, relit la page (clé primaire et FK, subset
compris), traduit la FK par Redis, retrouve la ligne de destination par sa propre nouvelle clé (une ligne dont
la clé n'a pas été publiée n'a pas été écrite), et écrit par `UPDATE` dans la transaction de la page. Le plan
neutre porte désormais la clé primaire de la table.

Un job copie une source vers une destination (décidé) : Athanor arrête au démarrage du run un job à plusieurs
destinations ou vers un autre SGBD que la source, au lieu d'échouer à la première table après le vidage.

**Test de recette rejoué** (job `demo-outil-franchise`, 66 tables, MySQL, worker à sa taille de page normale) :
Athanor **32,3 s**, Benthos **354,6 s** ; **65 tables identiques sur 66**, **zéro orphelin** dans les deux
destinations. La seule table qui diffère, `CHECK_LIST_COMMANDE_MONTEUR`, a 8 lignes de plus chez Benthos,
créées dans la source (14:12:07 et 14:12:40) après la fin du run Athanor et pendant celui de Benthos : écart de
la source vivante, pas des moteurs.

## Ordre

1. Banc MySQL (P1 puis P2, scénarios de droits compris), passage de Benthos et d'Athanor actuel : liste chiffrée
   des écarts de chacun.
2. Corrections avec le banc comme critère d'acceptation : intégrité référentielle, contrôle des droits, écarts
   du builder partagé (« Benthos amélioré »), puis le reste d'Athanor (Redis, identités, conversions).
3. Extension PostgreSQL et SQL Server (une passe via `session_replication_role` et `NOCHECK CONSTRAINT`).
4. Comparaison finale mesurée : durée, lignes/s, mémoire, exactitude contre l'attendu.
