# Contrôle des connexions et pré-vol du job (proposition)

Statut : **proposition, à valider** (point 8 de la reprise du 2026-09-18). Rien n'est codé. Prolonge
les décisions « Contrôle des droits selon le rôle de la connexion » et « Analyse du schéma avant
run » de [banc-essai-moteurs.md](banc-essai-moteurs.md).

## Ce que fait le code aujourd'hui (vérifié)

- Le **run** s'arrête à son démarrage sur un contrôle bloquant, pour MySQL et PostgreSQL, source et
  destination, moteur compris (activité `run-privileges`, après la génération des configs).
- Le **test de connexion** de l'UI (`CheckConnectionConfig` / `…ById`, utilisé par les formulaires de
  connexion et par la page de création de job) ignore le rôle et le moteur : il se connecte et liste
  des droits par table via `GetRolePermissionsMap`. Sur MySQL, cette lecture a le même défaut que
  l'ancien contrôle du run : un compte par rôle ou déclaré sur un hôte précis n'y montre **aucun**
  droit. L'utilisateur voit une liste vide, ou fausse.
- Le backend importe déjà des paquets de `internal/` : un code de contrôle partagé y a sa place.

## Proposition

### 1. Un seul code de contrôle, deux appelants

Les contrôles du run (`mysql.go`, `mysql_grants.go`, `postgres.go`) passent dans un paquet partagé
(`internal/connection-checks`, nom à discuter) qui rend une **liste de constats** typés plutôt que des
phrases : `{contrôle, niveau (bloquant | avertissement | information), table, manque, remède}` — le
remède étant l'instruction à exécuter (`GRANT TRIGGER ON …`, `GRANT SET ON PARAMETER …`).
L'activité du run et l'API l'appellent tous deux : ils ne peuvent plus diverger.

### 2. Test de connexion selon le rôle (contrat de l'API, à valider)

`CheckConnectionConfigRequest` et `…ByIdRequest` reçoivent, en champs **optionnels** (compatibles
avec les clients actuels) :

- `role` : source ou destination ;
- `engine` : Benthos ou Athanor (ce qu'Athanor seul exige, comme la suspension des FK sur
  PostgreSQL, n'est demandé qu'à lui) ;
- `tables` : les tables du job, quand l'appel vient de la configuration d'un job ;
- les options de destination qui changent ce qui est exigé (vidage avant écriture, init du schéma).

La réponse garde `is_connected`, `connection_error` et `privileges` (inchangés pour les clients
actuels), et ajoute `checks`, la liste des constats. Sans `role`, le comportement actuel est
conservé. `privileges` passe, pour MySQL, par `SHOW GRANTS` au lieu de la requête fautive.

### 3. UI

- Formulaire de connexion : un choix « tester comme source / comme destination », puis un constat par
  ligne (état, explication, remède copiable).
- Création et modification d'un job : les connexions choisies sont testées dans leur rôle, sur les
  tables du job, avec le moteur choisi. **Question produit** : un constat bloquant empêche-t-il
  d'enregistrer le job, ou seulement de le lancer (le run s'arrêtera de toute façon) ? Je propose
  l'avertissement à l'enregistrement : une destination peut recevoir ses droits après.

### 4. Pré-vol (analyse statique)

Décidé : produit par le calcul du plan, jamais par un scan de données. Proposition de mise en œuvre :

- `GenerateBenthosConfigs` sait déjà tout (colonnes, clés, index, chemins de subset, FK nullables sur
  le chemin, clés non lues, colonnes générées, triggers). Il produit aussi un **rapport de pré-vol**
  (mêmes constats typés que le point 1), stocké dans le run context du run.
- Un workflow « pré-vol » exécute la génération et le contrôle des droits **sans rien lire des tables
  ni écrire**, pour que l'UI montre le rapport avant le premier run ; le run réel refait le même
  calcul et le garde avec lui.
- Premiers constats, tous vérifiés par un cas du banc : clé primaire non lue par le job (pagination
  en un seul flux), table sans clé (reprise en double sous Benthos), FK nullable sur le chemin du
  subset, auto-référence sur une clé transformée, colonne générée mappée, trigger de destination,
  transformer constant sur une colonne unique, sortie plus longue que la colonne.

## Ordre proposé

1. Paquet de contrôle partagé, sans changement visible (l'activité du run l'utilise).
2. Contrat de l'API et implémentation dans le backend ; `privileges` MySQL corrigé.
3. UI du test de connexion, puis de la configuration du job.
4. Pré-vol.

## Questions pour toi

1. Contrat de l'API : d'accord pour étendre `CheckConnectionConfig*` (champs optionnels) plutôt
   qu'un nouvel appel ?
2. Constat bloquant à l'enregistrement d'un job : blocage ou avertissement ?
3. Le pré-vol dans cette branche, ou une PR à lui, une fois MySQL et PostgreSQL bouclés ?
