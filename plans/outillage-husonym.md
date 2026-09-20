# Husonym outillé : de quoi décider, pour l'UI comme pour un agent

Statut : **axes proposés le 2026-09-20, à challenger — rien n'est validé.** Ce document sert
à trancher les axes, pas à démarrer une implémentation. Chaque axe porte son objection, et
plusieurs objections sont des motifs suffisants d'abandon.

Point de départ : la branche `feat/ai-assisted-anonymization-config` (2026-07-08), qui a outillé
**une seule** décision — quel transformer sur quelle colonne. L'ambition ici est plus large :
que toute décision du produit repose sur un socle de faits lisible par l'UI, par la CLI et par un
agent. Les principes de cette branche sont bons et sont repris ; ses quatre défauts sont
structurants et sont corrigés.

Prolonge [controle-connexion-et-prevol.md](controle-connexion-et-prevol.md) (pré-vol, constats
typés) et [banc-essai-moteurs.md](banc-essai-moteurs.md) (le banc comme critère d'acceptation).

---

## 1. Ce que la branche a construit (vérifié dans le code)

Quatre briques, toutes réutilisables telles quelles :

**Une cascade déterministe, avec repli sûr.** `internal/ee/recommendations/category_transformer_map.go`
fait correspondre une catégorie PII et un nom de colonne (regex multilingues : `prenom`, `vorname`,
`nazwisko`, `plz`, `cap`…) à un transformer du catalogue, et signale par `IsGenericFallback` que la
correspondance est faible. `validity.go` filtre ensuite ce qui serait refusé à l'exécution, sur le
principe que **« une suggestion qui échouerait à `ValidateJobMappings` est pire que pas de
suggestion »**. C'est le meilleur principe de toute la branche.

**La preuve, pas le verdict.** `TransformerRecommendation` (proto, sur la branche) rend `category`,
`recommended_config`, `confidence` et surtout `evidence` : quel détecteur a parlé et sur quoi.
Une recommandation sans sa preuve est inexploitable — par un humain qui doit la valider comme par
un agent qui doit la pondérer.

**Le profil sans les valeurs.** `ColumnProfile` (`worker/.../piidetect/.../activities/profiler.go`,
resté dans une remise jusqu'au 2026-09-20, voir §5) décrit une colonne par sa forme
(`9999-AA-aa`), sa proportion de distincts, ses longueurs, son jeu de caractères et des détecteurs
de format — calculé localement, les valeurs échantillonnées ne quittant jamais le worker. C'est ce
qui rend l'assistance compatible avec la promesse du produit.

**Le brouillon quand le catalogue ne suffit pas.** `proposal.go` fait rédiger par un LLM un
transformer JavaScript, le compile avec goja avant de le montrer (`ProposalValidator`), et borne
les tentatives à `MaxProposalsPerRequest = 5`. La revue humaine est bloquante.

---

## 2. Quatre constats durs

### 2.1 Le repli de sécurité va dans le sens de la fuite

`FilterForValidity` replie sur **Passthrough** dès qu'il doute : catalogue muet, type incompatible,
transformer EE sans licence. Passthrough est le repli sûr d'un **sync** — la donnée arrive intacte.
Ce n'est pas le repli sûr d'une **anonymisation** — la donnée arrive en clair.

Ce n'est pas un accident isolé. L'auto-correction du playbook de recette (`detect_missing_columns`,
`auto_fix: true`) ajoute **elle aussi** toute colonne nouvelle en passthrough. Deux composants
écrits séparément, par des chemins différents, ont pris le même défaut par défaut. Un outil
d'anonymisation dont le défaut est « laisser passer » se dégrade à chaque évolution de schéma, en
silence, et personne ne le voit avant l'audit.

Ce qu'il faut à la place : un état explicite **« non décidé »**, distinct d'un passthrough choisi,
qui compte comme une dette visible et non comme une configuration.

### 2.2 Le filtre de validité ignore l'unicité et les clés étrangères

Sa signature est `(catalog, config, dataType, isGeneratedColumn, isIdentityColumn)`. Ni l'unicité,
ni les clés étrangères, ni la longueur maximale n'entrent dans la décision. Un transformer constant
posé sur une colonne `UNIQUE` passe le filtre sans broncher.

Autrement dit : la recommandation aurait validé `demo-of-station-login`, dont la constante `"demo"`
a fait échouer un job sur `Duplicate entry 'demo' for key 'USERS.UNQ_USER_LOGIN'` dès que la
station a compté plus d'un utilisateur. Ce n'est pas un cas limite, c'est le trou central : les
contraintes du schéma sont exactement ce que l'humain oublie et ce que la machine sait.

### 2.3 Le profil est calculé, puis jeté

Le commentaire de `ProposalRequest` le documente lui-même : le rapport piidetect persisté ne porte
que la catégorie et la confiance, jamais les valeurs **ni leur forme**. `ColumnProfile` vit le temps
d'un appel.

Conséquence : chaque surface qui voudra décider devra retourner lire la base. C'est coûteux, c'est
concurrent d'un run, et surtout cela rend impossible toute comparaison dans le temps — donc toute
détection de dérive.

### 2.4 On recommande pour une connexion, pas pour une intention

`GetJobMappingRecommendations(account_id, connection_id, optional job_id)`. Or `demo-outil-franchise`
et `sampling-web-stations` lisent **le même schéma** et veulent des transformers **opposés** : des
constantes de démonstration d'un côté, des valeurs variées de l'autre. Poser les premières sur le
second viole les contraintes d'unicité dès la deuxième ligne ; poser les seconds sur le premier
détruit la démonstration.

Une recommandation qui ignore le but du job se trompe la moitié du temps, et se trompe d'autant
plus qu'elle est confiante.

---

## 3. Les axes

Chaque axe porte ce qu'il parie et ce qu'on peut lui opposer. L'objection est la partie utile.

### Axe 1 — Le profil comme artefact de premier ordre

**Le pari.** Profil de colonne, contraintes et preuves sont calculés une fois, persistés, datés,
versionnés. L'UI, la CLI, le pré-vol et un éventuel agent lisent ce socle au lieu de retourner à la
base. La dérive devient calculable, puisqu'on a deux photographies à comparer.

**L'objection.** Un profil persisté est une **nouvelle surface de fuite**. `9999 9999 9999 9999`
avec 98 % de correspondance sur un détecteur Luhn, c'est un numéro de carte : la forme dit ce que la
valeur dirait. On aurait déplacé le secret, pas supprimé — et on l'aurait déplacé vers une table
plus facile à lire que la source. Il faut donc classer le profil au niveau de sensibilité de la
donnée elle-même, et lui donner une péremption : un profil de juillet ment en septembre.

### Axe 2 — Passer de la suggestion à la contrainte

**Le pari.** Ce dont une décision a besoin, ce n'est pas qu'on lui souffle `GenerateEmail`, c'est
qu'on lui dise **ce qui est interdit** : colonne unique donc aucune constante ; colonne référencée
par quatre clés étrangères donc transformation cohérente obligatoire ; colonne lue par le chemin du
subset donc pas de régénération ; longueur de sortie bornée. Ces interdits se dérivent du schéma,
ils sont vrais, et ils ne coûtent pas un jeton de LLM. Ils corrigent le constat 2.2.

**L'objection.** Ce n'est plus un filtre, c'est un solveur — et les contraintes se contredisent.
Unique + format préservé + longueur bornée + déterministe par clé : le catalogue peut n'avoir
**aucune** réponse. Il faut décider ce qu'on fait de l'ensemble vide avant d'écrire la première
ligne, sous peine de livrer un outil qui refuse sans proposer. C'est là, et seulement là, que le
brouillon JavaScript de `proposal.go` trouve sa vraie justification : non pas « le catalogue matche
mal », mais « le catalogue ne peut pas ».

### Axe 3 — L'essai comme primitive

**Le pari.** Appliquer un transformer sur N valeurs, compter les distinctes, confronter aux
contraintes — sans run. Les briques existent : `TryJavascriptRules`, `AnonymizeSingle`,
`ValidateJobMappings`, `GetColumnSampleValues`. Aujourd'hui, la seule vérification disponible est
un run complet, et `ColumnPreviewDialog` ne montre que les valeurs d'entrée.

**L'objection.** Un essai sur vingt valeurs ne prouve **rien** sur 250 000 lignes. Un essai qui
répond « OK » fabrique une assurance fausse, et une assurance fausse est pire que pas d'essai : elle
déplace la vigilance. Le seul signal honnête est un **taux de collision extrapolé** avec son
incertitude, jamais un feu vert. Si on n'est pas prêt à afficher l'incertitude, cet axe se retourne
contre nous.

### Axe 4 — L'intention du job comme entrée de premier ordre

**Le pari.** Démonstration, sampling, débogage, conformité : le but pilote la recommandation, la
portée de cohérence, le choix du moteur et ce qui compte comme dérive. Corrige le constat 2.4.

**L'objection.** Ajouter un champ « intention » prend une heure ; le rendre **conséquent** est un
travail de produit entier. S'il ne change pas visiblement ce que le produit propose, personne ne le
renseignera et il deviendra décoratif — une case de plus dans un formulaire déjà long. À ne faire
que si l'axe 2 en dépend explicitement et démontrablement.

### Axe 5 — Le diagnostic : de la prose à la cause typée

**Le pari.** Les erreurs de run sont aujourd'hui du texte libre. Les rendre typées, avec leur
remède, sert l'humain autant qu'un agent — c'est le même besoin que les constats du pré-vol
(#62), et la même forme : `{contrôle, niveau, table, manque, remède}`.

**L'objection.** Le coût n'est pas d'exposer, il est de **classer** les erreurs de MySQL et de
PostgreSQL — et le seul endroit du dépôt qui connaisse vraiment ces classes, c'est `bench/`. Cet axe
est une extension du banc avant d'être une fonctionnalité. Traité comme du cosmétique d'API, il
produira une taxonomie fausse, que personne ne relira.

### Axe 6 — Un contrat, plusieurs façades

**Le pari.** UI, CLI et une éventuelle surface pour agent lisent le même contrat de décision.

**L'objection.** C'est une promesse que le produit a **déjà ratée une fois**, et le constat est
écrit dans `controle-connexion-et-prevol.md` : le contrôle des droits du run et le test de connexion
de l'UI ont divergé, au point que l'UI affiche une liste de droits vide ou fausse. Trois façades sur
un contrat non partagé, ce sont trois vérités. **Le contrat partagé précède les façades ; il ne les
accompagne pas.**

Et si la façade en question est un serveur MCP, la question première n'est pas « quels outils » mais
**« qu'est-ce qu'un agent a le droit de lire »**. Une surface d'agent branchée sur un outil
d'anonymisation est, en une ligne de configuration, un canal d'exfiltration de données
personnelles : profils par défaut, valeurs brutes derrière un consentement explicite par connexion,
et tracé. Cette décision-là est antérieure à tout catalogue d'outils.

### Axe 7 — Rien sans banc

**Le pari.** La branche a son harness d'évaluation (8 langues, précision/rappel, étage déterministe
en CI). `bench/` couvre les moteurs. Il manque le banc des **recommandations sous contraintes** :
un jeu de schémas tordus (colonnes uniques, FK auto-référencées, colonnes générées, longueurs
serrées) avec la configuration attendue.

**L'objection.** Aucune — c'est la condition des six autres. Sans banc, chaque axe ci-dessus est une
opinion, et on a déjà la preuve dans ce dépôt que les opinions se périment en silence.

---

## 4. Ordre proposé

1. **Axe 1** (le socle) et **axe 2** (les contraintes) : ils rendent tout le reste vrai.
2. **Axe 7** (le banc), en même temps que 2 — une contrainte non testée est une contrainte fausse.
3. **Axe 5** (diagnostic typé), qui mûrit dans `bench/`.
4. **Axe 3** (l'essai), une fois qu'on sait afficher l'incertitude.
5. **Axe 6** (les façades) en dernier : une façade sur un contrat instable se refait deux fois.
6. **Axe 4** (l'intention), seulement s'il devient conséquent.

Le plus fragile de la liste est l'axe 3 : le plus demandé, le plus facile à livrer, et le seul qui
puisse activement nuire s'il est mal calibré.

---

## 5. Ce qui a déjà été fait

La remise locale du 2026-07-08 (33 fichiers, 2046 insertions), qui portait `profiler.go` — le socle
de l'axe 1 — n'était commitée nulle part. Elle est sauvée sur
`wip/ai-assisted-anonymization-profiler`, posée sur le sommet de la branche dont elle était issue :

- `2e892510` le travail produit (profil de colonne, dictionnaire durci, jeu d'évaluation élargi) ;
- `f9e245e1` la clé publique EE de développement local, **isolée et à ne jamais fusionner** — trois
  clés sont en jeu (`main` porte `bzAD8O1…`, la branche de juillet `tj2N7ZNo…`, la remise
  `1XBKv9sc…`), et la clé qui fait foi est dans Infisical.

Cette branche est un endroit où lire, pas une branche à fusionner : `main` a beaucoup avancé depuis
juillet (Athanor, montée des dépendances).

---

## 6. Questions pour toi

1. **Le repli (2.1).** D'accord pour un état « non décidé » distinct du passthrough, qui empêche
   d'enregistrer un job tant qu'il reste des colonnes détectées sensibles non tranchées ? Ou
   avertissement seulement, comme pour le pré-vol ?
2. **L'ensemble vide (axe 2).** Quand aucun transformer du catalogue ne satisfait les contraintes :
   on refuse, ou on propose systématiquement un brouillon JavaScript à relire ?
3. **Le profil persisté (axe 1).** Acceptable de stocker les formes, ou la surface de fuite
   l'emporte et il faut le recalculer à chaque fois ?
4. **La portée.** Commence-t-on par le socle partagé (axes 1, 2, 7) comme je le propose, ou veux-tu
   une façade visible tôt, quitte à la refaire ?
5. **L'intention (axe 4).** Y a-t-il d'autres buts que démonstration / sampling / débogage /
   conformité dans l'usage réel ?
