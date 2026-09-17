# Banc d'essai des moteurs (Benthos / Athanor)

Statut : conception validée le 2026-09-17. Rien n'est encore implémenté. Référence visuelle : page « Pièges par
SGBD » (schéma d'écriture par SGBD, structures de FK du banc, tableau des particularités).

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

## Conception de l'intégrité référentielle d'Athanor (améliore Benthos)

1. **FK nullables** : filtre à la lecture dans la source (`CASE WHEN EXISTS (sélection du parent) THEN col END`),
   sous-requête avec alias propres, sans `ORDER BY` ni `LIMIT`. Ne modifie jamais les lignes retenues, donc aucun
   cycle possible.
2. **FK obligatoires** : ligne écartée à l'écriture si son parent est absent de la destination (lecture indexée par
   lot). Les parents sont écrits avant (dépendances du workflow), la cascade est naturelle.
3. **Filet de fin de run** : comptage des orphelins sur toutes les FK des tables du job, virtuelles comprises.
   Avec `skipForeignKeyViolations` : `NULL` ou suppression en cascade ; sans : échec en listant les orphelins.

Les FK virtuelles sont traitées comme les réelles (Benthos écrit leurs orphelins).

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

## Piste à challenger : analyse complète du schéma avant run

Proposée, non décidée. Un rapport produit avant le premier run (et à la configuration du job) qui détecte ce que
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

### Destination et configuration du job (P2)

- Destination au schéma différent (colonne en plus `NOT NULL` sans défaut, ordre des colonnes différent,
  `initTableSchema` désactivé), triggers en destination, vues dans le schéma source.
- Mappings vers des colonnes disparues de la source, nouvelles colonnes non mappées.
- `onConflict` do nothing / update, avec clé unique autre que la clé primaire.
- Plusieurs destinations ; source et destination de SGBD différents.

### Exploitation (P3)

- Worker tué en cours de page (reprise idempotente) ; perte de connexion ; délai de requête source.
- `sql_mode` ou fuseau différents entre source et destination.
- Échelle : 1 million de lignes, table avec 20 FK nullables filtrées, sous-requêtes de subset à plusieurs jointures.

## Outil

- Générateur Go, graine fixe, taille paramétrable, une description de schéma pour les trois SGBD.
- Orchestration : chargement, création des jobs des deux moteurs, runs l'un après l'autre sur source figée,
  durées par table, comparaison ligne à ligne, contrôle des orphelins (FK virtuelles comprises), vérifications
  d'attendu par cas, rapport.
- Une commande `make`.

## Ordre

1. Banc MySQL (P1 puis P2, scénarios de droits compris), passage de Benthos et d'Athanor actuel : liste chiffrée
   des écarts de chacun.
2. Corrections avec le banc comme critère d'acceptation : intégrité référentielle, contrôle des droits, écarts
   du builder partagé (« Benthos amélioré »), puis le reste d'Athanor (Redis, identités, conversions).
3. Extension PostgreSQL et SQL Server (une passe via `session_replication_role` et `NOCHECK CONSTRAINT`).
4. Comparaison finale mesurée : durée, lignes/s, mémoire, exactitude contre l'attendu.
