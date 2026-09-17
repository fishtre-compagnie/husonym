# Athanor : plan d'exécution commun aux deux moteurs

Statut : décisions validées le 2026-09-17, implémentation en cours sur `feat/athanor-moteur-m1-m3`.

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

## Étapes

1. Portée de cohérence par job (proto, stockage, UI création + réglages) et clé obligatoire.
2. Plan neutre : type sérialisable, production dans `GenerateBenthosConfigs`, stockage en run context.
3. Athanor lit le plan : passes insert/update, requête avec subset, pagination par clé + jeton de
   continuation, `DO NOTHING` en reprise.
4. Écriture : destinations multiples et hétérogènes, identités/générées/défauts, conversions de types par SGBD.
5. Propagation des FK via Redis, identique à Benthos.
6. Benchmark comparatif sur un même plan (temps, lignes/s, mémoire) avant toute décision de retrait de Benthos.

Hors périmètre Athanor tant que non demandé : MongoDB, DynamoDB, S3/GCS, jobs de génération (Benthos reste le
moteur, choix explicite et journalisé).
