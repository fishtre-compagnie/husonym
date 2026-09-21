# Reprise du chantier « outillage des décisions » — état, décisions, suite

> Document d'entrée pour toute session qui reprend la branche `docs/plan-outillage-husonym` sans
> contexte. Mis à jour le 2026-09-21 au soir. Lire ensuite [outillage-husonym.md](outillage-husonym.md)
> (les sept axes et leurs objections), [reconciliation-job-par-le-run.md](reconciliation-job-par-le-run.md)
> (le modèle de revue actuel), puis la note mémoire `outillage-husonym-axes`.

---

## 1. Où en est la branche

`docs/plan-outillage-husonym` porte **29 commits au-dessus de `main`, rien n'est poussé.** Le nom de
branche est historique (elle a commencé par le seul document d'axes) : elle porte désormais du code.

Gate vert au dernier commit de code : `go build ./...`, `go test ./internal/... ./backend/... ./worker/...`
(≈ 2 700 tests), `golangci-lint` sur les paquets touchés, typecheck, eslint, prettier et jest (180) du
web, et les tests d'intégration **PostgreSQL + MySQL (29)** et **MSSQL (6)**
(`WORKER_INTEGRATION_TESTS_ENABLED=1 go test ./internal/integration-tests/worker/workflow/ -run
'Test_Workflow/(postgres|mysql|mssql)'`).

Entre `c7d3dcfd` et `e54ddb1b`, le typecheck du web échoue (le SDK n'a plus les anciennes RPC) : à
écraser en un commit si on veut chaque commit vert.

---

## 2. Ce qui est livré, par thème

**Le run tient le job à jour** (plan : [reconciliation-job-par-le-run.md](reconciliation-job-par-le-run.md))
- `664300ff` Le run écrit dans le job ce qu'il trouve (`ReconcileJobMappings`, sous verrou de la ligne,
  idempotent) : les colonnes nouvelles mappées par Passthrough et AutoMap, et la **suppression
  systématique** des mappings dont la colonne a disparu (sauf `ColumnRemovalStrategy = Halt`). Une
  source qui ne montre **aucune** colonne du job fait échouer le run au lieu de le vider.
- `c7d3dcfd` **Anonymize & Review** remplace Passthrough & Review : suggestion de `piidetect` avec la
  config du catalogue, passthrough si rien n'est suggéré ou si une clé/contrainte d'unicité couvre la
  colonne. **Fusionnée ensuite dans AutoMap** (commit suivant les guides) : l'AutoMap hérité (choix
  par le type : défaut, `NULL`, générateur) disparaît, `auto_map` porte ce comportement sous le nom
  « AutoMap & Review », MSSQL compris. Stratégies restantes : Halt, AutoMap & Review, Passthrough,
  Continue. **Journal** `job_mapping_changes` (ajouts, suppressions, changements de type) et
  **instantané des types** `job_source_columns`, écrit par tout run. `GetPendingMappingChanges`,
  `ReviewMappingChanges`. Anciens `unmapped_passthroughs`, `column_reviews`, acceptations par
  empreinte et codes de validation retirés ; **une seule migration** `20260921100000`.
- `e54ddb1b` Onglet Review sur le journal (marquer revu avec note, aperçu, modification sur la page
  Source), cloche et colonne de la liste des jobs.
- `0956d6df` Catalogue système sorti en paquet partagé (`internal/transformers/catalog`).

**Téléphones et formulaires d'options**
- `91c425ff` Option **`preserve_format`** sur `TransformPhoneNumber` : préfixe (`06`, `+33 6`),
  séparateurs et longueur gardés, autres chiffres permutés par **FF1** (`worker/pkg/fpe`, vecteurs
  NIST), aucune collision. Défaut du catalogue, suggestion des colonnes téléphone texte.
- `e3f98f26` / `4611fe5b` Composant partagé `TransformerForms/options/`, 22 formulaires migrés.

**Aperçu avant/après** — `ad19ed9b`, `54f9709a`, `2d929b7d` (règles JS en un seul essai).

**Prompt pour qu'un agent rédige une règle JS** — `efbf0610`, `5e14d40e`, `289e3fb0`.

**Réconciliation du schéma de destination** — `1c48ae56` (drapeau `postgresSchemaDrift` supprimé).

---

## 3. Décisions de l'utilisateur — ne pas les rediscuter

- Le repli d'une colonne nouvelle est **au choix de l'utilisateur** (stratégie opt-in), signalé, **jamais bloquant**.
- **Le run met à jour le job** qu'il concerne (colonnes nouvelles et supprimées), pour Passthrough,
  AutoMap et Anonymize & Review ; **suppression systématique** d'un mapping dont la colonne a disparu.
- Seule **AutoMap & Review** alimente une revue (ce qui a changé et ce qui a été choisi) ;
  Passthrough assume que la donnée reste la même. AutoMap et Anonymize & Review ne font **qu'une**
  stratégie, dont le repli est le **passthrough signalé** ; Halt et Continue restent.
- Changement de type **journalisé** ; colonnes à clé ou unicité en **passthrough** signalé ;
  écrasement d'un ajout du run par la page Source **accepté** (le run suivant le refait).
- La cloche porte **ce qui est à faire**, pas des événements : les **runs en échec sont un autre
  sujet** (de la notification), plus tard.
- On ne calcule pas à l'affichage (cela introspecterait les bases de production) : c'est le run qui écrit.
- Une **option** sur un transformer système plutôt qu'un nouveau transformer (« éviter d'en proposer
  12000 ») ; le produit n'a pas d'utilisateur externe, les contrats peuvent bouger.
- **Pas d'empilement de migrations** tant que rien n'est déployé.
- Défaut du moteur dans le prompt JS : **Athanor** (Benthos sera retiré à terme).
- Réconciliation PostgreSQL : **drapeau supprimé**, pas rendu configurable.
- La destination est **le miroir de la source, suppressions comprises**.
- Le brouillon JS est **rédigé par un agent** ; le produit lui fournit le prompt.

---

## 4. Ce que l'exécution réelle a appris

- `82880e97` : la stratégie n'était jamais enregistrée (structs de persistance par dialecte) → test par
  réflexion sur toutes les variantes du `oneof` ; même garde-fou ajouté pour **toutes** les variantes
  de `TransformerConfig` (`91c425ff`).
- Sous clé API worker, l'utilisateur est l'UUID nul : le run écrit les mappings par une requête
  dédiée qui ne touche pas `updated_by_id` (clé étrangère vers `users`).
- Bloblang reconstruit une fonction à chaque valeur quand un argument est dynamique : une clé tirée
  dans le constructeur changerait à chaque ligne.

---

## 5. Ouvert, par priorité

1. **Clé du téléphone sous Benthos** (question posée, sans réponse). Sous Athanor, clé du scope de
   cohérence ; sous Benthos — le moteur de la pile de dev — clé tirée **au démarrage du worker** :
   stable dans un run et entre tables, pas d'un redémarrage à l'autre. Option : dériver de
   `ATHANOR_CONSISTENCY_KEY` et du scope du job (mêmes sorties sur les deux moteurs), ce qui demande
   de définir la clé en dev (elle y est vide). Écart voisin, non traité : sous Athanor,
   `TransformPhoneNumber` sans l'option et les deux configs **E164** passent par `PhoneFaker`
   (`06`/`07` + 8 chiffres), qui ignore la config.
2. **Défaut de la stratégie** pour les nouveaux jobs : il reste « Continue ». Décision de l'utilisateur.
3. **Message d'erreur vide** des transformers système qui échouent (mise en forme de `jsonanonymizer`,
   préexistant).
4. **Hook Slack** (optionnel) sur l'apparition de changements à revoir.
5. Les **cinq questions** encore ouvertes de [outillage-husonym.md](outillage-husonym.md) §6.
6. **Pousser / ouvrir la PR** — rien de poussé ; attendre la validation de l'utilisateur.
7. Nettoyer les données de test (§6).

---

## 6. État de l'environnement local (au 2026-09-21 soir)

- **Pile de dev démarrée**, moteur **Benthos** (`ENABLE_ATHANOR_ENGINE` absent, `ATHANOR_CONSISTENCY_KEY`
  vide).
- **Base locale migrée à `20260921100000`** (la migration unique). Les deux anciennes migrations ont
  été redescendues à la main ; sauvegarde des tables d'avant dans le scratchpad de session
  (`avant-migration-unique.sql`). Revenir sur `main` demande de jouer le `down` de `20260921100000`.
- **Données de test** : compte personnel, connexions `review-e2e-source` (`test-prod-db`) /
  `review-e2e-dest` (`test-stage-db`), job `review-e2e` en **AutoMap & Review** (clé JSON `autoMap`,
  renommée à la main) et **exécuté deux fois** : `telephone`, `email_normalise` puis `mobile` mappés
  par les runs, trois changements en attente dans l'onglet Review. `test-prod-db.public.users` a gagné
  `email`, `email_normalise` (générée), `telephone`, `mobile` et perdu `commentaire`.
- ⚠ La connexion locale `demo-outil-franchise-source` pointe une **base de production** : n'exécuter
  aucun job existant hors `review-e2e`, n'altérer aucun schéma derrière ces connexions. Le début de son
  mot de passe a été affiché une fois dans une session : envisager une rotation.

**Pièges vérifiés**
- `air` laisse des **processus orphelins** (worker **et** API) : un binaire ancien continue de consommer
  la file ou tient le port de `dlv` (`bind: address already in use`). Vérifier
  `docker exec husonym-worker ps -o pid,lstart,args | grep 'worker serve'` (idem `husonym-api`,
  `tmp/mgmt serve`) : une seule génération d'heures. Sinon `docker restart`, **hors compilation**.
- Après une migration retirée ou renommée, l'API ne démarre plus tant que la base porte une version
  sans fichier : redescendre à la main, jamais `mgmt migrate down` (il redescend **tout**).
- `sqlc` ne supprime pas les fichiers générés des requêtes retirées, et `mockery` échoue tant que des
  mocks périmés empêchent le paquet de compiler : les supprimer avant de régénérer.
- Les chemins du web contiennent `(mgmt)` et `[account]` : prettier et eslint les lisent comme des
  motifs ; échapper (`\(mgmt\)/\[account\]`) ou lancer sur le dossier.
- Pas de `enginebench perf` sans demande (la machine chauffe) ; bancs de correction seulement.

---

## 7. Branches annexes

- `wip/ai-assisted-anonymization-profiler` sauve la remise du 2026-07-08 (dont `profiler.go`). Son
  commit `f9e245e1` porte une **clé publique EE de dev local : ne jamais le fusionner.**
- `stash@{0}` existe toujours ; supprimable une fois la branche `wip` vérifiée.
