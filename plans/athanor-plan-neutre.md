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
   **Décidé le 2026-09-18 : l'état ne vit que le temps d'une ligne.** Partagé entre les colonnes d'une ligne,
   jamais transmis à la suivante : la VM est recréée à chaque page (chez Benthos, tirée d'un pool), donc un
   état entre lignes n'est fiable dans aucun moteur, et il fait passer la donnée d'une personne dans la ligne
   d'une autre. La cohérence (même entrée, même sortie) passe par des fonctions déterministes offertes aux
   scripts (`pseudo.*`, Athanor seul). **Réalisé le 2026-09-18** : chaque exécution reçoit un objet global
   neuf hérité d'une VM scellée, dans le code partagé des deux moteurs et de l'API ; détail et mesures dans
   [athanor-regles-personnalisees.md](athanor-regles-personnalisees.md).

## Étapes

1. ✅ Portée de cohérence par job (proto, stockage, UI création + réglages) et clé obligatoire (`39a854f6`).
2. ✅ Plan neutre : type sérialisable, production dans `GenerateBenthosConfigs`, stockage en run context
   (`5175df77`).
3. ✅ MySQL et PostgreSQL / à faire SQL Server : Athanor lit le plan, pagination par clé + jeton de continuation,
   `DO NOTHING` en reprise, une passe FK suspendues (`e01e091c`) ; JavaScript dans une VM par table (`5f166f4e`).
   PostgreSQL suspend ses FK par `SET LOCAL session_replication_role = replica`, porté par la transaction de
   la page (`a528492f`).
4. ✅ MySQL et PostgreSQL : intégrité référentielle en trois étapes et banc d'essai (`bench/`, 60 cas dont 40
   neutres) : voir [banc-essai-moteurs.md](banc-essai-moteurs.md). Le plan porte les FK de la table, les
   colonnes générées et les clés publiées. Le contrôle des parents laisse la base comparer les clés avec le
   type et la collation de la colonne parente, sur les trois SGBD.
5. Écriture : ✅ colonnes générées et par défaut, `id = 0`, dates zéro (MySQL), identités PostgreSQL
   (`OVERRIDING SYSTEM VALUE`) ; à faire : destinations multiples et hétérogènes
   ([proposition](athanor-destinations-multiples-heterogenes.md)), identités SQL Server.
6. ✅ Propagation des FK via Redis (clés primaires transformées), mêmes hachages que Benthos ; auto-référence
   et cycle vers une clé transformée par la passe de mise à jour (étape 9).
7. ✅ MySQL et PostgreSQL, au démarrage du run (après la génération des configs, sur les tables et colonnes
   qu'elles écrivent) : contrôle des droits selon le rôle de la connexion, rôles et comptes par hôte compris,
   vidage, suspension et remise des triggers. À faire : test de connexion et configuration du job
   ([proposition](controle-connexion-et-prevol.md)), SQL Server.
8. ✅ Comparaison mesurée (`bench/perf`, 3,1 M lignes, 5 tours par moteur), sur les deux SGBD le
   2026-09-18 : Athanor 25,1 s contre 162,5 s sur MySQL (6,5 fois), 32,8 s contre 166,8 s sur PostgreSQL
   (5,1 fois), moins de mémoire, lignes écrites identiques table par table. Le portage PostgreSQL n'a rien
   coûté sur MySQL (mesure appariée). Détail et limites dans [banc-essai-moteurs.md](banc-essai-moteurs.md).
9. ✅ FK qui suit une clé transformée écrite plus tard (2026-09-18). Hors cycle, la table attend celle qui
   publie la clé (`c4ebbbc7`). Dans un cycle ou en auto-référence nullable, la passe d'insertion l'écrit `NULL`
   et Athanor exécute la passe de mise à jour que le plan porte déjà — **seulement** quand elle écrit une telle
   FK : elle traduit la FK et retrouve la ligne par sa nouvelle clé, comme Benthos. Reste refusée, avec un
   message explicite, l'auto-référence `NOT NULL` vers une clé transformée (Benthos échoue aussi).

Hors périmètre Athanor tant que non demandé : MongoDB, DynamoDB, S3/GCS, jobs de génération (Benthos reste le
moteur, choix explicite et journalisé).
