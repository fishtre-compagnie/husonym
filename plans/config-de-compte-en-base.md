# La configuration d'un compte vit en base

Statut : **proposé, non implémenté** (2026-09-23). Demandé pendant la revue de
[reconciliation-job-par-le-run.md](reconciliation-job-par-le-run.md) : la clé de dérivation de
l'anonymisation déterministe doit sortir des variables d'environnement, et le support doit
pouvoir accueillir ensuite la configuration SSO/OIDC.

---

## 1. Le besoin

Deux besoins qui se ressemblent assez pour partager un support, et assez peu pour qu'il faille le
dire :

- **La clé de cohérence.** Les deux moteurs en dérivent leurs sorties déterministes
  (`consistency.Deriver`, RFC §8) : Athanor pour tous ses transformers, Benthos pour la
  permutation de `TransformPhoneNumber` en `preserve_format`. Elle est aujourd'hui une variable
  du worker, donc **la même pour tous les comptes** d'un déploiement. Deux comptes dont les
  données n'ont rien à voir partagent la même dérivation ; et un compte ne peut pas faire tourner
  la sienne sans casser celle des autres.
- **La configuration SSO/OIDC**, à venir. Aujourd'hui l'authentification est entièrement en
  variables (`AUTH_BASEURL`, `AUTH_API_CLIENT_ID`, `AUTH_API_CLIENT_SECRET`…), donc **une seule
  pour tout le déploiement**. Un client qui amène son IdP demande le contraire.

Le point commun n'est pas « c'est de la config » — c'est **une valeur qui varie par compte, dont
une partie est un secret, et qu'un humain règle une fois**.

## 2. Ce qui existe déjà et qu'on réutilise

| Brique | Où | Ce qu'elle donne |
|---|---|---|
| `sym_encrypt.Encryptor` | `internal/encrypt/sym` | AES-GCM, clé dérivée d'un mot de passe, `Encrypt`/`Decrypt` sur des chaînes |
| `HUSONYM_SYM_ENCRYPTION_PASSWORD` | variable du backend | le secret maître du déploiement, déjà lu pour Slack |
| Le motif `config jsonb` + colonne générée | `account_hooks` (`hook_type`) | une table qui porte un `oneof` protobuf et se lit par type |
| Le verrou « seul le worker appelle » | `ReconcileJobMappings` | `s.cfg.IsHusonymCloud && !user.IsWorkerApiKey()` |

Rien à inventer côté cryptographie ni côté forme de table : le dépôt a déjà les deux.

**À noter, parce que ça change le raisonnement sur le risque** : les identifiants des connexions
(mots de passe des bases source et destination) sont aujourd'hui stockés **en clair** dans
`connections.connection_config`. Une clé de dérivation chiffrée en base n'est donc pas une
nouvelle classe d'exposition — elle est même mieux protégée que ce qui l'entoure. Le chantier
« chiffrer les identifiants de connexion » est un autre sujet, que ce support rendra plus facile.

## 3. Le modèle

### 3.1 La table

```sql
CREATE TABLE husonym_api.account_settings (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id uuid NOT NULL REFERENCES husonym_api.accounts(id) ON DELETE CASCADE,

  -- Le oneof sérialisé. Les champs secrets y sont déjà chiffrés (cf. 3.3).
  config jsonb NOT NULL,

  -- Dérivée du oneof, comme account_hooks.hook_type : c'est elle qui rend la table
  -- lisible et qui porte l'unicité.
  setting_type text GENERATED ALWAYS AS (...) STORED,

  created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
  created_by_user_id uuid NULL REFERENCES husonym_api.users(id) ON DELETE SET NULL,
  updated_by_user_id uuid NULL REFERENCES husonym_api.users(id) ON DELETE SET NULL,

  CONSTRAINT account_settings_one_per_type UNIQUE (account_id, setting_type)
);
```

**Une ligne par (compte, type de réglage)**, pas une ligne par clé/valeur : un réglage est un
objet cohérent (un IdP a un issuer, un client id et un secret qui ne valent rien séparément), et
un `oneof` donne le typage, la validation protobuf et l'évolutivité sans table à migrer.

### 3.2 Le contrat

```proto
message AccountSetting {
  oneof config {
    AnonymizationConsistency anonymization_consistency = 1;
    OidcProvider oidc_provider = 2;  // à venir
  }
}

message AnonymizationConsistency {
  // Chiffré au repos, absent des réponses à l'UI : voir 3.4.
  string derivation_key = 1;
}
```

Ajouter un réglage = ajouter une variante. Rien d'autre ne bouge : ni la table, ni les RPC, ni le
RBAC.

### 3.3 Le chiffrement

Les champs secrets sont chiffrés **champ par champ**, pas le blob entier, avec
`sym_encrypt` et `HUSONYM_SYM_ENCRYPTION_PASSWORD`. Ainsi l'issuer OIDC d'un compte reste lisible
en base pour du diagnostic, et seul le `client_secret` est opaque. La convention : un champ
secret porte l'annotation qui le dit, et une seule fonction traverse le message pour chiffrer ou
déchiffrer — pas de chiffrement dispersé dans chaque service.

Conséquence assumée : perdre `HUSONYM_SYM_ENCRYPTION_PASSWORD` rend les secrets illisibles. C'est
déjà le cas pour Slack.

### 3.4 Qui lit, qui écrit

| Appelant | Droit |
|---|---|
| Un administrateur du compte | écrit un réglage, lit tout **sauf les champs secrets** : absents de la réponse, remplacés par une empreinte (§8.3) |
| Le worker (clé d'API worker) | lit un réglage **en clair**, pour le compte du run en cours |
| Quiconque d'autre | rien |

Le worker lit déjà les mots de passe des bases qu'il synchronise ; lui donner la clé de son compte
n'ajoute pas de surface. L'UI, elle, n'a aucune raison de voir un secret qu'elle vient d'écrire :
on affiche « défini le … par … » et l'empreinte, avec un bouton pour remplacer.

## 4. La clé de cohérence comme premier occupant

### 4.1 Sa vie

**Décidé (2026-09-23) : elle est générée, pas saisie.** Ce n'est pas une valeur qu'on cherche à
écrire à la main ; on ne la fournit que pour en réutiliser une ancienne ou pour la changer.

- **Créée à la demande.** Au premier run d'un compte qui n'a **aucune** clé — ni réglage, ni
  variable de déploiement (cf. 4.2) — le backend en tire une (32 octets, `crypto/rand`), la
  chiffre, l'enregistre, la renvoie. Personne n'a rien à régler, et deux comptes ne partagent
  plus rien.
- **Fournie seulement pour une raison.** L'écriture accepte une clé donnée : reprendre celle d'un
  déploiement qu'on remplace, ou rejouer des sorties produites ailleurs. C'est le cas
  particulier, pas le chemin normal.
- **Jamais régénérée toute seule.** La régénérer change toutes les sorties : les destinations
  déjà peuplées ne correspondent plus. C'est un geste explicite, avec un avertissement qui le dit.
- **Sauvegardée avec la base.** Elle vit là où vivent les jobs qu'elle gouverne.

### 4.2 La cascade, pendant la transition

```
réglage du compte  →  ANONYMIZATION_CONSISTENCY_KEY  →  (ATHANOR_CONSISTENCY_KEY, déprécié)
```

Un déploiement qui a déjà une clé en variable **continue de produire les mêmes sorties** : tant
qu'aucun réglage de compte n'existe, la variable gagne. La bascule d'un compte vers sa propre clé
est un geste explicite, parce qu'elle change ses sorties.

C'est la condition qui rend la génération automatique sans danger, et il faut la lire dans ce
sens : **on ne génère que lorsque la cascade ne donne rien**. Sur un déploiement existant qui
porte la variable, rien ne bouge et aucune clé n'est créée ; sur un déploiement neuf, où la
variable n'est pas posée, chaque compte reçoit la sienne au premier run — l'isolation par compte
devient le défaut sans que personne ait à la demander. Cela répond à la question 2 du §8 : il n'y
a pas de bascule des comptes existants, il y a une variable qu'on retire le jour où l'on veut
qu'ils basculent.

### 4.3 Ce que le worker en fait

Le worker demande la clé du compte au démarrage de `SyncTable`, comme il demande déjà le job.
Elle entre ensuite là où la variable entrait : `EngineConfig.ConsistencyKey` devient une valeur
**par run** et non plus par process. Le drapeau `hasConsistencyKey` que lit AutoMap suit le même
chemin : « ce compte a de quoi dériver », et non plus « ce worker a de quoi dériver ».

Coût : un appel RPC de plus par activité de sync. À mettre dans le même aller-retour que la
lecture du job, qui a déjà lieu (`useAthanorForJob`), plutôt que d'en ajouter un.

## 5. L'OIDC comme deuxième occupant, pour vérifier que le support tient

Ce qu'il faudrait y mettre : `issuer`, `client_id`, `client_secret` (secret), `scopes`,
`redirect_url`. Ce que ça demande en plus du support : que l'**API** choisisse le vérificateur de
jeton selon le compte, ce qui est un chantier à part — mais le support, lui, ne bouge pas. C'est
le test qu'on voulait : ajouter un réglage n'ajoute qu'une variante.

Un point à trancher le moment venu : l'OIDC d'un compte est nécessaire **avant** de savoir à quel
compte appartient celui qui se connecte. Il faudra donc une entrée par domaine ou par slug de
compte, résolue avant l'authentification — ce qui n'est pas un réglage « de compte » au même sens.
À regarder quand le sujet viendra, sans tordre le support d'ici là.

## 6. Ce que ça ne fait pas

- **Pas de réglage de déploiement en base.** Ce qui vaut pour tout le monde (URL de Temporal,
  limites, activation d'un moteur) reste en variables : c'est de l'infrastructure, pas du produit.
- **Pas de rotation automatique**, pas de versionnement des secrets. Quand la rotation viendra,
  elle demandera une seconde clé lue en parallèle le temps d'un run — à concevoir alors.
- **Pas de KMS.** Le secret maître reste une variable. Le jour où un KMS entre, il remplace
  `sym_encrypt` derrière la même interface.

## 7. Découpage proposé

1. Migration + `oneof` + RPC de lecture/écriture + chiffrement, sans aucun occupant : le support
   nu, avec ses tests.
2. La clé de cohérence : génération à la demande, reprise d'une clé fournie, cascade, lecture
   par le worker, bascule de `EngineConfig` vers une valeur par run.
3. L'UI : une page de réglages du compte, un secret qui s'affiche « défini le … », un
   remplacement.
4. (Plus tard, hors de ce plan) OIDC ; chiffrement des identifiants de connexion sur le même
   mécanisme.

Les étapes 1 et 2 se tiennent seules et n'exigent pas la 3 : un compte sans réglage retombe sur la
variable, comme aujourd'hui.

## 8. Questions tranchées

1. **Création à la demande, ou geste explicite ?** → **à la demande** (2026-09-23). La clé se
   génère ; on ne la saisit que pour en reprendre une ancienne ou la changer (§4.1). Sans danger
   pour l'existant parce qu'on ne génère que si la cascade ne donne rien (§4.2).
2. **Bascule des comptes existants.** → il n'y en a pas : la variable de déploiement continue de
   gagner tant qu'elle est posée. La retirer est le geste qui fait basculer (§4.2).
3. **Les champs secrets sont-ils masqués ou absents** de la réponse à l'UI ? → **absents**, avec
   une **empreinte** à côté : les premiers octets d'un condensat de la clé, jamais de la clé.
   Montrer « •••• 4f2 » en découvrant la fin du secret le rend plus facile à deviner, alors qu'une
   empreinte remplit le seul besoin réel — reconnaître que deux déploiements partagent la même
   clé, ou qu'on vient bien de remplacer celle qu'on croyait.

## 9. Ce qui reste à décider quand le chantier démarre

- **Le nom du réglage dans l'UI.** « Clé de cohérence » est le terme du code ; ce n'est pas
  forcément celui qui parle à qui l'ouvre une fois.
- **Qui a le droit de la remplacer** : tout administrateur de compte, ou un rôle plus étroit ?
  Remplacer la clé rend inexploitables toutes les destinations déjà peuplées du compte.
