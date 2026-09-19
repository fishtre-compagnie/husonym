# Montée des dépendances, septembre 2026

Statut : **faite** sur `feat/athanor-moteur-m1-m3` (PR [#30](https://github.com/fishtre-compagnie/husonym/pull/30)),
un commit par écosystème ou par majeure. Mandat : monter toutes les dépendances, majeures comprises,
sans ménager d'anciens clients — la compatibilité est supprimée plutôt que maintenue.

Ce document garde les décisions et les pièges ; le détail des versions est dans les messages de commit.

## Ce qui est monté

| Lot | Contenu |
|---|---|
| Génération | oapi-codegen 2.8, buf 1.73, plugins `protocolbuffers/go` 1.36.12, `connectrpc/go` 1.21, `bufbuild/es` 2.15, python et pyi 36.2, `grpc/python` 1.84, protoc-gen-connect-openapi 0.27.2 |
| API | connect 1.21, grpchealth 1.5, protovalidate v1 |
| Go, socle | Go 1.27.1, casbin v3, slogt v2, backoff v7, openai-go v3, mockery v3 |
| Go, sensible | go-jwt-middleware v3, stripe-go v86, go-auth0 v3, charm v2 |
| Worker | Temporal SDK 1.49, pgx 5.11, mysql 1.10.1, mssql 1.11, mongo-driver v2, goja, benthos 4.80, redpanda connect 4.110, pg_query_go v6 |
| Frontend | TanStack Table v9, npm 12 |
| Images | Temporal 1.32 (serveur, admin-tools, UI 2.54.1), debian trixie, otel collector 0.161, loki 3.7, promtail 3.6, bases de test |

## Décisions

- **protovalidate v1 par l'amont.** `internal/connectrpc/validate` était une copie de
  `connectrpc/validate-go`, faite quand l'amont retardait sur protovalidate. L'amont v0.7.0 tourne sur
  `buf.build/go/protovalidate` v1.4.0 avec le même comportement par défaut : la copie est supprimée.
  `buf.lock` pointe le commit protovalidate dont le module Go généré est issu, pour que les SDK TS et
  Python décrivent les règles que le serveur applique.
- **backoff v7 : une seule enveloppe.** L'intercepteur connect dupliquait `backoffutil.Retry`. Règle
  retenue pour l'erreur rendue : l'erreur de l'opération quand le retry s'arrête sur elle (permanente
  ou tentatives épuisées), le `*RetryError` quand c'est le contexte ou le budget de temps, puisqu'il
  porte les deux.
- **Stripe sur `stripe.Client`.** `client.API` est déprécié. Validation sans compte Stripe : les cinq
  appels de facturation exercés contre `stripe-mock`, qui vérifie chaque requête contre la spec OpenAPI.
- **Auth0 et JWT validés pour de vrai.** Aucun test ne faisait passer un jeton réel : un faux tenant
  local (client credentials, `GET /api/v2/users/{id}`) et un test bout en bout signé RS256 les couvrent.
- **Temporal sans `auto-setup`.** L'image s'arrête à 1.29.7 et `temporalio/docker-compose` est archivé.
  La composition suit `temporalio/samples-server` : un conteneur crée ou met à jour les schémas, un
  autre le namespace, les deux idempotents. Les volumes Temporal de dev ont été recréés (saut de six
  mineures, décision de l'utilisateur).
- **Plugin OpenAPI gardé en local.** Il existe sur le BSR, mais un plugin distant ne peut pas lire
  `husonym.openapi.template.yaml`.
- **npm 12.** `allowScripts` liste esbuild, unrs-resolver et `@parcel/watcher`, sans épingler la
  version pour qu'une montée ne saute pas silencieusement leur script.

## Corrections sorties de la montée

- **`pg_sequences` (produit).** La lecture des séquences joignait la vue `pg_sequences`, dont la colonne
  `last_value` fait évaluer `pg_sequence_last_value()` sur **toutes** les séquences de la base. Une
  séquence supprimée au même instant par une autre session fait échouer la requête
  (« could not open relation with OID »), donc la troncature d'une destination PostgreSQL pendant qu'un
  autre job modifie la base. La requête lit maintenant `pg_catalog.pg_sequence` par OID.
- **Borne du backoff DynamoDB.** La sortie était encore en backoff v4 ; ses deux limites sont reportées,
  et la durée écoulée ne compte plus l'attente entre deux lots (elle était remise à zéro à la libération
  du pool, pas à sa prise).
- **Visibilité des colonnes de la table d'activité.** L'effet réécrivait l'état initial au montage ; la
  visibilité découle maintenant de `isError`.
- **Outillage de test.** MySQL 8.4 refuse de créer une clé étrangère vers une clé non unique, que la 8.0
  acceptait et conserve à la migration : le serveur de test tourne avec
  `restrict-fk-on-non-standard-key=OFF` pour garder ce cas. Le PostgreSQL de test passe à 500 connexions
  (une douzaine de jobs en parallèle, pools sans limite, contre 100 par défaut).

## Laissé de côté

- **eslint 10** : `eslint-config-next` 16.3.5 ne l'accepte pas. **TypeScript 7** : `typescript-eslint`
  exige `<6.1` et `ts-jest` `<7`.
- **Images du banc** (`bench/compose.bench.yml` : `mysql:8.0`, `postgres:16`) : figées, sinon la
  baseline n'est plus comparable.
- **`tilt/temporal/temporal.yaml`** : encore en Temporal 1.21 avec `auto-setup`, visiblement plus
  entretenu. À supprimer ou à refaire, pas touché ici.
- **`xwb1989/sqlparser`** : plus maintenu depuis 2018, toujours dans le graphe.
- **`GetDatabaseSchema` sous DDL concurrente.** La requête parcourt toutes les colonnes de la base et
  évalue `pg_get_expr()` sur chaque valeur par défaut ; ces fonctions lisent le catalogue courant, pas
  l'instantané, d'où des « cache lookup failed for type » quand un autre client supprime un type. La
  corriger demande de restreindre la lecture aux schémas du job, donc de changer `GetSchemaColumnMap`
  pour tous les dialectes : à décider à part.
