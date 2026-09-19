# Temporal

Composition reprise de l'exemple PostgreSQL + Elasticsearch de
[temporalio/samples-server](https://github.com/temporalio/samples-server/tree/main/compose),
qui remplace le dépôt `temporalio/docker-compose`, archivé.

L'image `temporalio/auto-setup` n'est plus publiée après la 1.29.7. Deux conteneurs à usage unique
font désormais ce qu'elle faisait au démarrage du serveur :

- `temporal-admin-tools` crée ou met à jour les schémas PostgreSQL et Elasticsearch
  (`scripts/setup-postgres-es.sh`, idempotent) ; le serveur attend qu'il ait fini ;
- `temporal-create-namespace` crée le namespace `default` s'il manque
  (`scripts/create-namespace.sh`) ; le worker attend qu'il ait fini.

Monter la version du serveur : changer `TEMPORAL_VERSION` et `TEMPORAL_ADMINTOOLS_VERSION` dans `.env`,
une version mineure à la fois sur des données existantes.
