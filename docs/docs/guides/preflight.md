---
title: Pré-vol d'un job
description: Ce qu'un run rencontrera, dit avant de le lancer, à partir du plan du run et des droits de ses connexions
id: preflight
hide_title: false
slug: /guides/preflight
---

## À quoi ça sert

Un run peut échouer au milieu, après avoir déjà écrit d'autres tables : une colonne que la
destination calcule elle-même, un compte qui ne peut pas écrire, une valeur plus longue que sa
colonne. Le pré-vol le dit **avant** : il calcule le plan du run exactement comme le run le
calcule, et demande aux connexions ce que leur rôle exige.

Rien n'est lu dans les tables et rien n'est écrit : ni le job (AutoMap n'y ajoute aucune
colonne), ni la destination, ni la clé de cohérence du compte. Seuls le catalogue des bases et les
droits des comptes sont consultés.

## Où le voir

- **Onglet Overview du job**, carte **Pre-flight** : le bouton **Check now** lance le pré-vol. Il
  ne se lance pas tout seul, puisque chaque pré-vol se connecte aux bases du job.
- **Trigger Run** : le pré-vol passe d'abord. Pendant qu'il tourne, une fenêtre le dit (quelques
  secondes, jusqu'à une minute sur un gros schéma). Si rien ne bloque ni n'avertit, le run part
  aussitôt ; sinon la fenêtre montre les constats :
  - un **avertissement** laisse le choix (**Run anyway**) ;
  - un constat **bloquant** ne laisse que **Close** : le run s'arrêterait de toute façon à son
    démarrage ;
  - un pré-vol qui n'a pas pu aboutir (connexion injoignable, pas de worker) laisse le choix : le
    run refait le contrôle à son démarrage.
- **Page d'un run**, section **Pre-flight of this run** : ce que le run a trouvé à son démarrage.
  Elle est ouverte quand le run s'y est arrêté.

Le pré-vol demande, en plus du droit de voir le job, celui de voir les connexions et ce qu'elles
stockent : il s'y connecte avec leurs identifiants.

## Les niveaux

| Niveau        | Sens                                                                                       |
| ------------- | ------------------------------------------------------------------------------------------ |
| Bloquant      | Le run échoue quelles que soient les lignes : il s'arrête à son démarrage, avant d'écrire. |
| Avertissement | Le run peut échouer, ou copier autre chose que prévu, selon les lignes.                    |
| Note          | Ce que le run fait, et qu'il vaut mieux savoir.                                            |

## Les constats

| Constat                                                                                                                             | Niveau                                                        |
| ----------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| Droit manquant sur une connexion (lecture, écriture, vidage, triggers, suspension des FK), avec l'instruction `GRANT` qui l'accorde | Bloquant                                                      |
| Le moteur ne sait pas exécuter le job (fonctions `pseudo.*` sous Benthos, cas refusés par Athanor)                                  | Bloquant                                                      |
| Colonne calculée par la destination qui recevrait une valeur                                                                        | Bloquant sous Benthos ; note sous Athanor, qui ne l'écrit pas |
| Valeur plus longue que la colonne de destination (colonne source plus large, UUID, SHA-256, catégories)                             | Avertissement                                                 |
| Même valeur sur toute une clé unique (catégorie unique)                                                                             | Avertissement                                                 |
| Table sans clé réécrite en double quand Benthos reprend une écriture                                                                | Avertissement                                                 |
| Table lue en un seul flux (pas de clé, ou clé non lue par le job)                                                                   | Note                                                          |
| Triggers de destination mis de côté pendant le run, puis remis                                                                      | Note                                                          |
| Référence vers une ligne que le subset écarte, écrite à `NULL`                                                                      | Note                                                          |

Seuls MySQL et PostgreSQL sont analysés pour l'instant.
