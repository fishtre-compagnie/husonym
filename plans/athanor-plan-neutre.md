# Athanor : plan d'exécution commun aux deux moteurs

Statut : décisions validées le 2026-09-17, implémentation en cours sur `feat/athanor-moteur-m1-m3`.
La suite (banc d'essai, intégrité référentielle, contrôle des droits) est détaillée dans
[banc-essai-moteurs.md](banc-essai-moteurs.md).

## Objectif

Pouvoir exécuter **le même travail** avec Benthos et avec Athanor pour comparer leur efficacité réelle, puis
retirer Benthos s'il est battu sur tous les points. Athanor ne doit donc dépendre d'aucun type Benthos.

## Constat (vérifié dans le code)

Athanor recalcule seul, depuis le job, une version appauvrie de ce que `GenerateBenthosConfigs` calcule déjà :

| Manque d'Athanor | Référence Benthos |
|---|---|
| Passes `update` ignorées (FK circulaires **et** tables avec subset) : la table est recopiée à chaque passe | `internal/runconfigs/builder.go` (`buildConstraintHandlingConfigs`) |
| Subset non propagé par les FK | `worker/pkg/select-query-builder/querybuilder.go` |
| Une seule passe par table, alors que l'activité a 10 min par défaut | pagination + jeton de continuation, `tablesync/workflow` |
| Pas d'idempotence en reprise | `ON CONFLICT DO NOTHING` quand `isRetry` (`output_sql_insert.go`) ; `TruncateOnRetry` est déprécié et ignoré |
| Colonnes d'identité / générées / `DEFAULT` | `getColumnDefaultProperties`, `OVERRIDING SYSTEM VALUE`, `IDENTITY_INSERT` |
| Conversions de types à l'écriture (JSON, tableaux PG, map, uuid/money/tsvector en octets) | `worker/pkg/benthos/sql/processor_husonym_{pgx,mysql,mssql}.go` |
| Destinations multiples, SGBD différents source/destination | `internal/benthos/benthos-builder/generate-benthos.go` |
| Propagation d'une clé primaire transformée vers les FK | Redis (`hset` côté parent, `hget` côté fille), clé par `jobId`+`runId` |

## Décisions

1. **Plan neutre** : un plan par table (sérialisable, sans type Benthos) calculé une fois par
   `GenerateBenthosConfigs` à partir de `RunConfig` et `SelectQuery` (déjà neutres), stocké en run context.
   Benthos garde sa config, issue du même calcul.
2. **Propagation des FK : Redis conservé.** Une dérivation déterministe native peut l'éviter plus tard pour les
   transformers qui s'y prêtent, mais « graine par valeur » ne rend pas n'importe quel transformer déterministe
   (`uuid.NewString()` dans GenerateUUID/GenerateEmail/TransformEmail/GenerateSHA256Hash, `time.Now()` dans les
   timestamps, JavaScript, PII, IA).
3. **Portée de cohérence choisie par job**, défaut `run` :
   - `run` : sorties non reliables d'un run à l'autre (comportement Benthos) ;
   - `job` : stables entre les runs d'un même job ;
   - `account` : stables dans tout le compte — c'est de la **pseudonymisation** au sens du RGPD.

   Le scope inclut toujours l'identifiant (run, job ou compte) : deux comptes ne partagent jamais de sorties.
4. **Clé de dérivation obligatoire** (`ATHANOR_CONSISTENCY_KEY`) : plus de clé de démo codée en dur, qui
   permettait de retrouver par force brute les valeurs à faible entropie.
5. **Une passe par table, FK suspendues pendant l'écriture**, sur les trois SGBD : Husonym a les droits complets
   sur la destination (serveur autonome ou primaire, jamais un réplica). MySQL `SET FOREIGN_KEY_CHECKS=0`
   (session, fait) ; PostgreSQL `SET LOCAL session_replication_role = replica` (transaction, superutilisateur,
   suspend aussi les triggers utilisateur) ; SQL Server `NOCHECK CONSTRAINT ALL` puis
   `WITH CHECK CHECK CONSTRAINT ALL` (portée table, la revalidation refuse les orphelins). Les passes de
   mise à jour du plan deviennent sans effet.
6. **Faire mieux que Benthos quand c'est possible** (principe utilisateur) : les deux moteurs doivent rester
   comparables, donc une amélioration placée dans le calcul partagé (builder de requêtes, plan, contrôles) profite
   aux deux ; « Benthos amélioré » est la référence à battre.
7. **Transformers JavaScript d'une table dans une seule VM**, ligne par ligne, dans l'ordre des mappings, avec
   `input` : des transformers réels gardent un état partagé entre colonnes via le global `neosync` (fait).

## Étapes

1. ✅ Portée de cohérence par job (proto, stockage, UI création + réglages) et clé obligatoire (`39a854f6`).
2. ✅ Plan neutre : type sérialisable, production dans `GenerateBenthosConfigs`, stockage en run context
   (`5175df77`).
3. ✅ MySQL / à faire PostgreSQL et SQL Server : Athanor lit le plan, pagination par clé + jeton de continuation,
   `DO NOTHING` en reprise, une passe FK suspendues (`e01e091c`) ; JavaScript dans une VM par table (`5f166f4e`).
4. ✅ MySQL : intégrité référentielle en trois étapes et banc d'essai (`bench/`, 58 cas) : voir
   [banc-essai-moteurs.md](banc-essai-moteurs.md). Le plan porte les FK de la table, les colonnes générées et
   les clés publiées.
5. Écriture : ✅ colonnes générées et par défaut, `id = 0`, dates zéro (MySQL) ; à faire : destinations multiples
   et hétérogènes, identités PostgreSQL et SQL Server, conversions de types par SGBD.
6. ✅ Propagation des FK via Redis (clés primaires transformées), mêmes hachages que Benthos ; clé
   auto-référencée vers une clé transformée refusée (une passe).
7. ✅ MySQL, au démarrage du run : contrôle des droits selon le rôle de la connexion. À faire : test de connexion
   et configuration du job (proto `CheckConnectionConfig`), PostgreSQL, SQL Server.
8. Comparaison mesurée sur le banc (temps, lignes/s, mémoire, exactitude) avant toute décision de retrait de
   Benthos : mode `bench/perf` à écrire.

Hors périmètre Athanor tant que non demandé : MongoDB, DynamoDB, S3/GCS, jobs de génération (Benthos reste le
moteur, choix explicite et journalisé).
