# Le run met le job à jour — refonte de la revue des colonnes

Statut : **réalisé le 2026-09-21** (`0956d6df` catalogue partagé, `664300ff` réconciliation,
`c7d3dcfd` Anonymize & Review et journal, `e54ddb1b` front, `9563e42f` guides). Réponses de
l'utilisateur aux questions de la §5 : journaliser les changements de type, passthrough pour les
colonnes à contrainte, libellé « Anonymize & Review », écrasement par la page Source accepté, run
réel de `review-e2e` autorisé. Et **une seule migration** (`20260921100000`) au lieu d'en empiler :
rien n'était déployé. **Puis, même jour : Anonymize & Review fusionnée dans AutoMap**
(« AutoMap & Review », repli passthrough signalé) ; l'AutoMap hérité, qui choisissait par le type,
est retiré. Là où ce document dit « Anonymize & Review », lire « AutoMap & Review ». Prolonge
[reprise-outillage-husonym.md](reprise-outillage-husonym.md) ; remplace le modèle livré par
`a2a3f2f5` (le run enregistre ce qu'il copie) et `6ee1b455` (acceptation par empreinte).

---

## 1. Décisions de l'utilisateur (2026-09-21)

- **Le run qui détecte une colonne nouvelle ou supprimée met à jour le job**, pour toutes les
  stratégies qui laissent le run continuer.
- **Suppression systématique** du mapping d'une colonne disparue de la source, qu'il ait été posé
  par un humain ou par un run. C'est le principe du miroir : la destination suit la source.
- Par stratégie de colonne nouvelle :

| Stratégie | Le run écrit dans le job | Revue |
|---|---|---|
| Halt | rien (le run s'arrête) | — |
| Passthrough | un mapping `Passthrough` | aucune : on assume que la donnée reste la même |
| AutoMap (amont) | le mapping qu'il choisit aujourd'hui (défaut, `NULL`, générateur par type) | aucune : on le laisse se débrouiller |
| **Automap & Review** (remplace Passthrough & Review) | le transformer suggéré, sinon `Passthrough` | **ce qui a changé et ce qui a été choisi** |

- La stratégie de **suppression** existante (`ColumnRemovalStrategy`) garde son sens : `HaltJob`
  arrête le run sans toucher au job ; `ContinueJob` supprime désormais le mapping du job.

---

## 2. Le modèle cible

### 2.1 Où le run écrit

Au début de `GenerateBenthosConfigs`, avant de construire quoi que ce soit, le worker appelle une
RPC `ReconcileJobMappings(job_id, run_id, added[], removed[])`, puis relit le job. Le builder cesse
d'ajouter des mappings éphémères (`getAdditionalPassthroughJobMappings`,
`getAdditionalJobMappings`, `removeMappingsNotFoundInSource`) : il lit un job déjà à jour.

La RPC, côté serveur, sous verrou de la ligne du job (`SELECT … FOR UPDATE`) :
- **ajoute** un mapping pour chaque colonne absente du job, **sans jamais écraser** un mapping
  existant (un humain a pu le poser entre-temps) ;
- **retire** le mapping de chaque colonne annoncée disparue ;
- est **idempotente** : Temporal peut rejouer l'activité.

Conséquence voulue : un choix automatique est fait **une fois**. Il ne dérive plus d'un run à
l'autre quand les règles de `piidetect` évoluent.

À vérifier à l'implémentation : qu'Athanor relit bien le job après la réconciliation (le plan
d'Athanor est censé être le même que celui de Benthos, à confirmer dans le code).

### 2.2 Le choix d'« Automap & Review »

Pour chaque colonne nouvelle, par nom et type, sans lire aucune valeur de la source :
1. colonne générée par la base → `GenerateDefault` (comme aujourd'hui) ;
2. clé primaire, clé étrangère ou `UNIQUE` → `Passthrough` (un transformer suggéré pourrait
   casser la contrainte et faire échouer le run ; le choix sous contraintes est l'axe 2) ;
3. `piidetect` a une suggestion → la **config par défaut du catalogue** pour cette source (ce que
   l'UI applique déjà : `preserve_format` pour le téléphone) ;
4. sinon → `Passthrough`.

Le catalogue système (`transformers-service/system_transformers.go`) sort du service vers un
package partagé, lu par le backend et par le worker : une seule définition des défauts.

### 2.3 Le journal des changements (Automap & Review seulement)

Nouvelle table `husonym_api.job_mapping_changes` :

| colonne | rôle |
|---|---|
| `job_id`, `run_id`, `created_at` | quel run a changé quoi, quand |
| `schema`, `table`, `column` | la colonne |
| `kind` | `added` ou `removed` |
| `transformer` (jsonb) | ce qui a été posé (`added`) ou ce qui a été retiré (`removed`) |
| `data_type`, `category` | le type lu et la catégorie détectée, pour la revue |
| `reviewed_at`, `reviewed_by`, `note` | la revue |

**À revoir** = changement non revu d'un job en Automap & Review. Un changement `added` dont le
mapping a été modifié depuis par un humain est **revu de fait** (quelqu'un a décidé) : la requête
le compare au mapping courant, aucune écriture croisée n'est nécessaire.

Aucun champ n'est ajouté à `JobMapping` : la revue vit dans le journal, pas dans la config.

---

## 3. Ce que la refonte retire

| Livré | Devient |
|---|---|
| table `unmapped_passthroughs`, RPC `SetJobUnmappedPassthroughs`, rapport du worker | supprimés : le job dit ce qui passe en clair |
| table `column_reviews`, RPC `Get/Set/RemoveColumnReview`, empreinte de type/catégorie | supprimés : « accepter » = revoir un changement |
| `GetPendingColumnReviews`, `MapUnmappedColumns` | remplacés par `GetPendingMappingChanges`, `ReviewMappingChanges` |
| filtre `job_id` et codes d'acceptation de `ValidateJobMappings` | retirés |
| `passthrough_pending_review` (3 dialectes) | `automap_pending_review`, même position du `oneof` |
| onglet Review, `AcceptPassthroughsDialog`, cloche, colonne « To review » | onglet refait sur le journal ; cloche et colonne gardées, autre source |

Migrations : une nouvelle migration supprime les deux tables et crée le journal (les migrations
`20260921100000` et `20260921110000` sont appliquées en local ; on ne les réécrit pas). Aucune
donnée à reprendre : rien de cela n'a quitté la branche.

Gardé : `job_util.LooksSensitive` (catégorie et rang), `PreviewColumnTransformer`, le composant
d'options, `preserve_format`.

---

## 4. Découpage et taille

1. **Catalogue partagé** — déplacement sans changement de contenu. Petit.
2. **Réconciliation par le run** — RPC, verrou, idempotence ; worker qui l'appelle et relit le job ;
   builder allégé ; Passthrough et AutoMap écrivent désormais dans le job. **Le plus gros et le plus
   risqué** : il change le comportement des stratégies existantes. Tests d'intégration
   PostgreSQL + MySQL.
3. **Automap & Review** — proto, persistance (le test d'aller-retour couvre la nouvelle variante),
   choix de la §2.2, journal.
4. **Retraits** de la §3 — backend, proto, migrations.
5. **Front** — onglet Review sur le journal (ajouts avec le transformer choisi, suppressions ;
   confirmer, modifier, aperçu avant/après), cloche, colonne de la liste, libellés de stratégie.
6. **Docs et guides.**

Ordre de grandeur : six commits, comparable à ce qu'a coûté le modèle actuel (a2a3f2f5 → 77b3757d).

---

## 5. Questions ouvertes

1. **Changement de type d'une colonne existante** : l'empreinte actuelle rouvrait la revue quand le
   type ou la catégorie détectée changeait. Faut-il que le run l'inscrive au journal (`kind =
   type_changed`) ? Sinon ce signal disparaît avec `column_reviews`.
2. **Colonnes à contrainte** (§2.2, point 2) : passthrough signalé, d'accord ?
3. **Libellé** : « Automap & Review » côtoie l'`AutoMap` amont, qui fait autre chose. Garder le nom,
   ou « Anonymize & Review » ?
4. **Écriture concurrente inverse** : un utilisateur qui enregistre la page Source ouverte avant le
   run écrase l'ajout du run ; le run suivant le rajoute et le journalise de nouveau. Acceptable ?
5. **Vérification réelle** : elle demande de lancer `review-e2e` (`test-prod-db` → `test-stage-db`),
   un job existant — autorisation nécessaire.
