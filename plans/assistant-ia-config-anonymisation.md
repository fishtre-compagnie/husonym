# Plan — Assistance IA à la configuration des règles d'anonymisation

> **Statut** : proposition validée en revue d'architecture, à implémenter
> **Date** : 2026-07-08 (révisé le même jour)
> **Contrainte directrice** : **coût le plus faible possible**. Cible par défaut : LLM
> local (coût récurrent nul). Une API externe reste acceptable si son coût est
> marginal — l'architecture est provider-agnostique et minimise structurellement le
> volume envoyé au LLM, quel qu'il soit.

---

## 1. Objectif

Assister l'utilisateur dans la phase de configuration des mappings d'anonymisation :
détecter automatiquement les colonnes contenant des données sensibles et **proposer**
(jamais appliquer d'office) un transformer adapté par colonne, avec justification et
niveau de confiance. L'utilisateur revoit, accepte ou rejette.

Quand aucun transformer système ne convient (format métier spécifique : plaque
d'immatriculation, référence client structurée…), l'assistant propose la **création
d'un user-defined transformer** (brouillon JavaScript généré, revu et validé par
l'utilisateur) — voir §4.5.

## 2. Décision d'architecture

Pipeline en cascade, entièrement auto-hébergé, orchestré par le workflow Temporal
`piidetect` existant (`worker/pkg/workflows/ee/piidetect/`) :

```
Étage 1 — Métadonnées (local, instantané, gratuit)
  regex sur noms de colonnes (existant) + dictionnaire multilingue européen
  + heuristiques de types
        │ colonnes classées avec confiance ≥ seuil → sortent du pipeline
        ▼
Étage 2 — Petit LLM local (sidecar llama.cpp, CPU, gratuit)
  classe uniquement les colonnes restées ambiguës
        ▼
Recommandation (backend, déterministe)
  rapport de détection + catalogue SystemTransformer + contraintes schéma
  → suggestions TransformerConfig par colonne (confiance + justification)
        ▼
Revue humaine (frontend) → application aux mappings → ValidateJobMappings
```

Presidio reste un **détecteur optionnel** (étage intermédiaire sur valeurs
échantillonnées) pour les clients qui le déploient déjà — hors périmètre du MVP.

### Choix du provider LLM : abstraction unique, deux modes

Le client LLM est construit sur l'API OpenAI-compatible (`LLM_BASE_URL` + `LLM_API_KEY`
+ `LLM_MODEL`) — le SDK `openai-go` déjà présent supporte `option.WithBaseURL`, le code
de l'activité est identique dans les deux modes :

- **Mode local (défaut recommandé)** : sidecar llama.cpp (§3). Coût récurrent nul,
  aucune donnée ne sort. La tâche principale (classification de colonnes, sortie JSON
  contrainte) est étroite : un 4B quantisé suffit.
- **Mode API externe (option)** : pour les clients sans les ~4 CPU / 6 Gi du sidecar,
  ou pour la génération de code de transformer (§4.5) où un modèle plus capable aide.
  Choisir un modèle d'entrée de gamme (ordre de grandeur 0,1–0,6 $/M tokens, type
  `gpt-4o-mini`, Gemini Flash, Mistral Small…).

Le coût du mode externe est structurellement écrasé par l'architecture elle-même, pas
seulement par le prix unitaire du modèle :

- **cascade** : seules les colonnes non résolues par l'étage 1 (regex/dictionnaire)
  atteignent le LLM — typiquement 10–30 % des colonnes ;
- **scan incrémental** existant (fingerprint SHA-256 par table) : les tables inchangées
  ne sont jamais rescannées ;
- **budget tokens** existant (tiktoken, plafond par requête) et défaut « noms de
  colonnes seuls » (pas de valeurs) → ~1–2k tokens in / ~0,5k out par table.

Ordre de grandeur en mode externe avec `gpt-4o-mini` : **< 0,001 $ par table
scannée**, soit quelques dizaines de centimes pour un rescan complet d'une base de
500 tables — et seulement au moment des scans, rien en usage courant. Le mode reste un
choix de déploiement du client, documenté avec ses implications RGPD (DPA, transfert).

## 3. Le sidecar LLM

### Serveur d'inférence

**`llama-server` (llama.cpp)**, image officielle `ghcr.io/ggml-org/llama.cpp:server`.

- API OpenAI-compatible (`/v1/chat/completions`).
- CPU-only, pas de dépendance GPU/CUDA — déployable partout.
- Supporte `response_format: json_schema` (grammaire GBNF) → **sortie JSON garantie
  conforme au schéma**, ce qui compense la fragilité des petits modèles sur le format.
- Alternative si le client préfère : Ollama (même API) ; le code n'en dépend pas.

### Modèle

Candidats à évaluer (ordre de préférence), quantisation **Q4_K_M** :

| Modèle | Taille | Licence | RAM approx. |
|---|---|---|---|
| Qwen3-4B-Instruct | 4B | Apache 2.0 | ~3,5 Go |
| Phi-4-mini-instruct | 3,8B | MIT | ~3 Go |
| Qwen3-1.7B (fallback machines contraintes) | 1,7B | Apache 2.0 | ~1,5 Go |

Critères de choix : licence permissive (usage commercial), **qualité multilingue
européenne** (schémas nommés en français, allemand, espagnol, italien… selon les
clients — critère qui favorise les Qwen, très multilingues), score sur le jeu
d'évaluation (§7).
Le modèle est un simple fichier GGUF monté en volume : changer de modèle = changer
un fichier + une variable d'env, aucun changement de code.

### Performance attendue (dimensionnement)

- Prompt par table : noms de colonnes + types (+ échantillons si opt-in) ≈ 1–2k tokens.
- Sortie : JSON de classification ≈ 200–500 tokens/table.
- Sur 8 threads CPU : ~10–30 tok/s de génération → **~15–45 s par table**, uniquement
  pour les colonnes non résolues par l'étage 1 (minorité).
- Exécution **asynchrone** via Temporal (déjà le cas) : la latence n'impacte pas l'UI ;
  concurrence par table déjà pilotée par `TABLE_PII_DETECT_MAX_CONCURRENCY`.

## 4. Modifications au projet

### 4.1 Worker (`worker/`)

1. **Client LLM configurable** — `worker/internal/cmds/worker/serve/serve.go:425` :
   remplacer la construction OpenAI par :
   - `LLM_BASE_URL` (ex. `http://husonym-llm:8080/v1`), `LLM_API_KEY` (optionnel,
     compat `OPENAI_API_KEY` conservée), `LLM_MODEL`.
   - `LLM_BASE_URL` absent → étage LLM désactivé proprement (pattern `(client, ok, err)`
     de `getPresidioClient`, `backend/internal/cmds/mgmt/serve/connect/cmd.go:1308`).
   - L'interface `OpenAiCompletionsClient`
     (`worker/pkg/workflows/ee/piidetect/workflows/table/activities/activities.go:29`)
     et son mock restent inchangés.
2. **Sortie contrainte** — dans `DetectPiiLLM`, passer un `response_format` de type
   `json_schema` (schéma : `{output: [{field_name, category, confidence}]}`) au lieu du
   simple `json_object` : indispensable pour la fiabilité d'un petit modèle. Adapter le
   prompt (`piiDetectionPrompt`) : plus court, avec exemples few-shot couvrant
   plusieurs langues européennes (le modèle doit classer une colonne `geburtsdatum` ou
   `codigo_postal` aussi bien qu'`email`).
3. **Cascade** — dans `TablePiiDetect`
   (`worker/pkg/workflows/ee/piidetect/workflows/table/workflow.go`) : n'envoyer au LLM
   que les colonnes dont la confiance étage 1 est < `PII_DETECT_CONFIDENCE_THRESHOLD`
   (défaut 0,8). Réduit le volume LLM de 70–90 % sur des schémas bien nommés.
4. **Étage 1 enrichi** — compléter les ~20 regex de `activities.go` (`init()`, l.179) :
   - dictionnaire **multilingue européen** de noms de colonnes, structuré **par langue
     dans des fichiers de données séparés** (un fichier par langue, embarqués via
     `go:embed`) pour rester extensible sans toucher au code. Couverture initiale :
     fr, en, de, es, it, nl, pt, pl (`prenom`/`vorname`/`nombre`/`imie`,
     `date_naissance`/`geburtsdatum`/`fecha_nacimiento`, `num_secu`/`nino`/
     `codice_fiscale`/`pesel`, `iban`, `tel*`, `adresse`/`anschrift`/`direccion`, …) ;
   - identifiants nationaux européens à format fort (NIR français, Codice Fiscale,
     PESEL, DNI/NIE, BSN, Steuer-ID…) : regex de format sur les valeurs échantillonnées
     quand le sampling est actif ;
   - heuristiques de types (`inet`, `citext`, dates nommées `*naissance*`/`*birth*`/
     `*geburt*`).
5. **Échantillonnage à trois niveaux** — remplacer le booléen `ShouldSample` par un
   mode (proto : enum `SamplingMode`, compat ascendante `ShouldSample=true → RAW`) :

   | Mode | Ce que voit le LLM | Statut RGPD |
   |---|---|---|
   | `NONE` (défaut) | noms de colonnes + types | aucune donnée traitée |
   | `PROFILE` (défaut dès que l'échantillonnage est activé) | un **profil de forme** par colonne, calculé localement | profil agrégé, anonyme par construction |
   | `RAW` (opt-in explicite) | valeurs brutes (plafond existant : 5 lignes) | traitement de données perso, réservé au mode local ou couvert par DPA |

   **Le mode `PROFILE`** : les lignes sont bien échantillonnées (mécanisme
   `getSampleData` existant), mais restent dans le worker. On en dérive par colonne,
   en pur Go :
   - **motifs de forme** : chaque caractère est remplacé par sa classe (`9` chiffre,
     `A`/`a` lettre, séparateurs conservés) — `1 86 03 75 116 001 23` →
     `9 99 99 99 999 999 99`, `jean.dupont@mail.fr` → `aaaa.aaaaaa@aaaa.aa` ; on
     transmet les k motifs les plus fréquents avec leur proportion ;
   - **statistiques** : longueurs min/moy/max, alphabet, cardinalité relative,
     proportion de nulls ;
   - **verdicts des détecteurs locaux** : « format IBAN valide sur 5/5 », « regex NIR
     sur 4/5 », « checksum Luhn OK »… (les regex de format de l'étage 1 travaillent,
     elles, sur les valeurs brutes — en local, donc sans contrainte).

   C'est le point important : pour toutes les catégories **à format fort**
   (`national_id`, `financial`, `contact` téléphone/email, `authentication`), la forme
   porte l'intégralité du signal de décision — le mode `PROFILE` ne dégrade
   pratiquement pas la classification, et le prompt compact aide même le petit modèle.
   La perte ne concerne que le **contenu sémantique** (texte libre, colonnes mal
   nommées dont seules les valeurs révèlent la nature : distinguer une colonne de
   prénoms d'une colonne de villes). Pour ce résidu, le profil signale
   « texte lexical non structuré » et le LLM classe sur le nom de colonne ; si c'est
   insuffisant, `RAW` reste disponible en connaissance de cause.

### 4.2 Backend (`backend/`)

1. **Nouveau RPC** `JobService.GetJobMappingRecommendations`
   (proto `backend/protos/mgmt/v1alpha1/job.proto`) :
   - lit le dernier rapport `piidetect` de la connexion source (réutilise la logique de
     `GetPiiDetectionReport`, `backend/services/mgmt/v1alpha1/job-service/runs.go:1168`,
     y compris résultats partiels en cours de run) ;
   - retourne `TransformerRecommendation[]` : `{schema, table, column, category,
     recommended_config (TransformerConfig), confidence, evidence[]}` où `evidence`
     porte la source (`REGEX | DICTIONARY | LLM`) et le détail lisible
     (ex. « regex `(^|_)email` sur le nom de colonne »).
2. **Table de correspondance déclarative** catégorie×type → `TransformerConfig`,
   nouveau module `internal/ee/recommendations/` :
   - `contact`/email → `TransformEmail` ; `contact`/téléphone → `TransformPhoneNumber` ;
   - `personal`/prénom → `GenerateFirstName` (idem nom, ville…) ;
   - `national_id`, `financial`, `authentication` → `GenerateUuid`/`TransformString`
     selon type ; non-PII → `Passthrough`.
3. **Filtre de validité serveur** : ne suggérer que des configs compatibles avec les
   `DataTypes` du catalogue (`system_transformers.go`) et les contraintes PK/FK/
   generated/identity — répliquer la logique de
   `frontend/.../SchemaTable/transformer-handler.ts`. Une suggestion qui casserait
   `ValidateJobMappings` est pire que pas de suggestion.
4. **Gating** : pattern EE existant (`cascadelicense.IsValid()` + booléen dans le
   `Config` du service, cf. `cmd.go:727`).

### 4.3 Frontend (`frontend/apps/web`)

1. **Bouton « Suggestions IA »** dans
   `components/jobs/SchemaTable/SchemaTable.tsx`, à côté d'« Apply Default
   Transformers » :
   - rapport `piidetect` récent disponible → appel `getJobMappingRecommendations` ;
   - sinon → proposer de lancer un scan (job `piidetect`, flux existant) avec suivi de
     progression.
2. **Panneau de revue** `RecommendationsReviewSheet.tsx` (nouveau) : tableau
   colonne / catégorie / transformer suggéré / confiance / justification ; accept/reject
   ligne à ligne + « accepter tout au-dessus de X % ». Application via le mécanisme de
   `useOnTransformerBulkUpdateClick` (écriture `mappings.${index}.transformer`), puis
   validation serveur habituelle.
3. **Badges d'alerte** dans `JobMappingTable` : pastille sur toute colonne classée PII
   laissée en `Passthrough`.

### 4.4 Proposition de création de transformers sur mesure

Quand la détection identifie une colonne sensible dont le format ne correspond à aucun
transformer système (référence métier structurée, identifiant composite, format
national absent du catalogue…), la recommandation inclut une **proposition de création
d'un user-defined transformer**, en s'appuyant sur l'infrastructure existante :

1. **Proto** — étendre `TransformerRecommendation` :
   `optional NewTransformerProposal proposal = 8` avec `{name, description,
   javascript_code, rationale}`. Émis uniquement quand aucun transformer du catalogue
   ne passe le filtre de validité avec une correspondance sémantique.
2. **Génération** — le LLM produit un brouillon de code pour `TransformJavascript`
   (le mécanisme user-defined existant) à partir du nom, du type et — si opt-in — de
   la *forme* des échantillons (longueur, alphabet, séparateurs), avec pour consigne de
   préserver le format tout en détruisant l'information (ex. permuter les chiffres
   d'une référence en gardant le préfixe métier).
3. **Validation serveur** — le brouillon passe systématiquement par
   `TransformersService.ValidateUserJavascriptCode` (RPC existant) avant d'être
   présenté ; un code invalide est silencieusement abandonné (fallback :
   suggestion `TransformString`/`GenerateUuid` générique).
4. **Frontend** — dans le panneau de revue, action « Créer ce transformer » : ouvre le
   formulaire de création user-defined existant **pré-rempli** (nom, description,
   code), où l'utilisateur relit le code, le teste sur des valeurs d'exemple (aperçu
   existant des transformers), puis enregistre via `CreateUserDefinedTransformer`.
   Le transformer créé est alors affecté à la colonne comme n'importe quel autre.
5. **Sécurité** — le code généré s'exécute dans le même bac à sable que le JavaScript
   écrit à la main par les utilisateurs (aucune surface nouvelle), mais la **revue
   humaine du code est bloquante** : jamais de création ni d'affectation automatique.
   Le panneau signale explicitement que le code est généré par IA.

Note capacité : la génération de code est la tâche la plus exigeante du plan. Le 4B
local peut produire des brouillons simples ; c'est le cas d'usage qui justifie le plus
le mode API externe (§2) — configurable indépendamment si besoin (un
`LLM_CODEGEN_MODEL` optionnel, même base URL par défaut).

### 4.5 Déploiement

1. **Compose** : nouvel overlay `compose/compose-llm.yml` (modèle des overlays DB) :
   service `husonym-llm` = `llama-server` + volume GGUF + healthcheck ; variables
   `LLM_*` injectées dans le worker.
2. **Helm** : bloc optionnel dans `worker/charts/worker/values.yaml` (`llm.enabled`,
   `llm.baseUrl`, `llm.model`) + Deployment sidecar optionnel (requests : 4 CPU / 6 Gi
   recommandés, fonctionne dégradé en dessous).
3. **Distribution du modèle** : script `scripts/fetch-llm-model.sh` (téléchargement
   HuggingFace + vérification sha256) ; jamais de poids dans l'image Docker.
4. **Docs** : page « Assistance IA — quelles données sont traitées, où, comment
   désactiver » (docs.husonym.com) — pour les DPO des clients.

## 5. Confidentialité & RGPD

- **Mode local (défaut)** : rien ne sort de l'infrastructure du client. Aucun DPA
  fournisseur d'IA nécessaire, pas de transfert hors UE.
- **Mode API externe (opt-in)** : noms de colonnes/types et profils de forme partent
  vers le fournisseur choisi ; les valeurs brutes uniquement en mode `RAW` (double
  opt-in) → DPA requis, à documenter dans le registre des traitements du client. L'UI
  affiche clairement le mode actif.
- Par défaut, seuls les **noms de colonnes et types** sont traités. L'échantillonnage
  activé passe d'abord par le mode `PROFILE` (§4.1.5) : les valeurs sont lues
  localement dans le worker et réduites à des profils de forme agrégés — anonymes par
  construction, donc hors du champ des données personnelles transmises, y compris en
  mode API externe. Seul le mode `RAW` (opt-in explicite, plafonné) constitue un envoi
  de valeurs et est documenté comme traitement au sens RGPD.
- Journalisation : jamais de valeurs échantillonnées dans les logs.
- Human-in-the-loop obligatoire : l'outil **assiste**, il ne garantit pas la
  conformité — mention explicite dans l'UI et la doc.

## 6. Phasage

| Phase | Contenu | Livrable |
|---|---|---|
| **P1 — Socle** | Client LLM configurable (`LLM_*`), sortie `json_schema`, cascade seuil, dictionnaire multilingue | `piidetect` fonctionne à coût nul en local |
| **P2 — Recommandation** | RPC `GetJobMappingRecommendations`, table catégorie→transformer, filtre de validité | Suggestions consommables par API |
| **P3 — UI** | Bouton, panneau de revue, badges | Boucle complète utilisateur |
| **P4 — Industrialisation** | Overlay compose + Helm, script modèle, doc, éval CI | Déployable client |
| **P5 — Création de transformers** | Proposition + génération de code JS, validation, formulaire pré-rempli (§4.4) | Couverture des formats hors catalogue |

P1 et P2 sont parallélisables (worker vs backend). Le choix final du modèle se fait en
P1 via le jeu d'évaluation. P5 vient en dernier : elle dépend de toute la boucle et
c'est la fonctionnalité la plus exigeante en capacité modèle.

## 7. Évaluation & tests

- **Jeu d'évaluation versionné** (`internal/ee/recommendations/testdata/`) : schémas
  synthétiques **multilingues** (au minimum fr, en, de, es, it, nl, pt, pl — mêmes
  langues que le dictionnaire) avec vérité terrain (colonne → catégorie attendue),
  incluant les cas difficiles (`data`, `col_17`, JSON, texte libre, faux amis type
  `contact_id`, abréviations locales type `plz`, `cap`, `cp`).
- Métriques : précision/rappel par catégorie **et par langue** (pour détecter une
  couverture inégale du dictionnaire ou du modèle), **rappel prioritaire** (un faux
  négatif = fuite potentielle de PII). Seuil d'acceptation avant bascule de modèle.
- **Comparaison des modes d'échantillonnage** : le jeu d'évaluation est exécuté en
  `NONE`, `PROFILE` et `RAW` pour mesurer l'écart réel de qualité — c'est la preuve
  chiffrée que `PROFILE` peut être le défaut sans perte de décision (et l'indicateur
  des rares cas où `RAW` apporte quelque chose).
- CI : étage 1 testé à chaque PR (pur Go) ; étage LLM évalué à la demande contre un
  endpoint (job manuel), pour comparer les candidats modèles et détecter les régressions
  de prompt.
- Tests unitaires : mock `OpenAiCompletionsClient` existant réutilisé ; tests de la
  table de correspondance et du filtre de validité.

## 8. Risques assumés

| Risque | Mitigation |
|---|---|
| Qualité d'un 4B < gpt-4o-mini sur colonnes très ambiguës | Cascade : le LLM ne traite que le résidu ; sortie contrainte json_schema ; few-shot multilingue ; jeu d'éval pour choisir le meilleur candidat |
| Faux négatifs (PII manquée) | Positionnement « assistance, pas garantie » ; badges Passthrough-sur-PII ; rappel prioritaire dans l'éval |
| Biais d'automatisation (tout accepter) | Pas de « tout accepter » sans seuil de confiance ; mise en évidence des basses confiances |
| RAM/CPU insuffisants chez certains clients | Fallback modèle 1,7B ; étage LLM désactivable → étage 1 seul reste fonctionnel |
| Divergence matrice type→transformer client/serveur | Noté comme dette ; à terme, source unique côté serveur |
| Granularité des 6 catégories vs ~50 transformers | Assumé en V1 : l'IA propose la classe de traitement, l'humain affine les options ; les cas hors catalogue basculent vers la proposition de création (§4.4) |
| Code JS généré incorrect ou faussement anonymisant | Validation `ValidateUserJavascriptCode` systématique, test sur valeurs d'exemple dans le formulaire, revue humaine bloquante, mention « généré par IA » |
| Dérive de coût en mode API externe | Cascade + incrémental + budget tokens plafonnent le volume ; coût par scan loggé/affiché ; mode local toujours disponible |
