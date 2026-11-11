-- Oracle fixture of the Init Table Schema option: a database modelled on the legacy MySQL 5.7
-- production database "allopneus" (5.7.42, sql_mode='', explicit_defaults_for_timestamp=OFF,
-- lower_case_table_names=0, latin1 database with latin1/utf8/utf8mb4 tables side by side).
--
-- The oracle test (internal/integration-tests/worker/workflow/mysql_schema_oracle_test.go)
-- loads it on a source server, syncs it with Init Table Schema to an empty destination, and
-- requires the destination to be the same database: same SHOW CREATE DATABASE / TABLE /
-- TRIGGER / FUNCTION, same information_schema, same bytes in every row.
--
-- Only MySQL 5.7 syntax is used. The same file runs on MySQL 8.4 and MariaDB 11.4 (servers
-- set to an empty sql_mode and explicit_defaults_for_timestamp=OFF, like 5.7): utf8 is
-- read there as utf8mb3, FLOAT(M,D), ZEROFILL and display widths are deprecated but accepted.
--
-- Run as one multi-statement string (no DELIMITER: the server parses BEGIN ... END itself).
--
-- What it covers:
--   * database DEFAULT CHARSET latin1 COLLATE latin1_swedish_ci;
--   * tables in latin1_swedish_ci, utf8_general_ci, utf8_unicode_ci, utf8_bin,
--     utf8mb4_general_ci, utf8mb4_unicode_ci, and columns whose charset/collation differs
--     from their table's (latin7_general_ci, ascii_bin, ascii_general_ci, latin1 in utf8mb4,
--     utf8mb4 in utf8);
--   * ENGINE InnoDB and MyISAM; ROW_FORMAT COMPACT, DYNAMIC, FIXED (MyISAM) and implicit, and
--     an InnoDB table COMPACT without the clause (created under an older default);
--   * INT(10) UNSIGNED ZEROFILL, TINYINT(1) UNSIGNED ZEROFILL, FLOAT UNSIGNED, FLOAT(7,3),
--     DOUBLE, DECIMAL (signed/unsigned), ENUM, SET, TEXT, MEDIUMBLOB, BINARY, VARBINARY, JSON,
--     YEAR;
--   * TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP, DEFAULT
--     '0000-00-00 00:00:00', NULL DEFAULT NULL, and with no clause at all (implicit defaults of
--     explicit_defaults_for_timestamp=OFF); DATETIME and DATE with zero defaults; defaults '',
--     '0', NULL, negative and decimal numbers, ENUM/SET defaults;
--   * table and column comments with accents and apostrophes;
--   * PRIMARY KEY (simple, composite), tables without one (holding duplicate rows), UNIQUE,
--     composite and prefix (col(10)) indexes, FULLTEXT on MyISAM and on InnoDB;
--   * FOREIGN KEY ON DELETE CASCADE / SET NULL / RESTRICT / default, ON UPDATE CASCADE, a
--     self-referencing foreign key;
--   * names with capitals (lower_case_table_names=0);
--   * BEFORE INSERT triggers in a set order (FOLLOWS), one of them not idempotent (it counts
--     its runs: if the destination fires it while the rows are copied, the copy differs), a
--     BEFORE UPDATE trigger, two stored functions;
--   * data: latin1 accents and cp1252-only characters (Œ, €), bytes 0x81/0x9D that latin1 maps
--     to C1 controls, Baltic text in latin7, 4-byte emoji in utf8mb4, zero dates, binary
--     values with 0x00/0xFF, NULL next to '' and empty blobs.

SET NAMES utf8mb4;

DROP DATABASE IF EXISTS `allopneus`;
CREATE DATABASE `allopneus` DEFAULT CHARACTER SET latin1 COLLATE latin1_swedish_ci;
USE `allopneus`;

CREATE TABLE `Statut_Commande` (
    `id` TINYINT(3) UNSIGNED NOT NULL AUTO_INCREMENT,
    `Code` CHAR(3) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `Libelle` VARCHAR(50) NOT NULL DEFAULT '' COMMENT 'Libellé affiché à l''internaute',
    `Ordre` TINYINT(1) UNSIGNED ZEROFILL NOT NULL DEFAULT '0',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uniq_Code` (`Code`)
) ENGINE=InnoDB DEFAULT CHARSET=latin1 COLLATE=latin1_swedish_ci ROW_FORMAT=COMPACT
  COMMENT='États possibles d''une commande';

CREATE TABLE `Categorie` (
    `id` INT(10) UNSIGNED NOT NULL AUTO_INCREMENT,
    `Parent_id` INT(10) UNSIGNED DEFAULT NULL,
    `Nom` VARCHAR(100) NOT NULL,
    `Slug` VARCHAR(120) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    `Description` TEXT,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uniq_Slug` (`Slug`),
    KEY `idx_Parent` (`Parent_id`),
    CONSTRAINT `fk_Categorie_Parent` FOREIGN KEY (`Parent_id`) REFERENCES `Categorie` (`id`)
        ON DELETE SET NULL ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8 COLLATE=utf8_general_ci ROW_FORMAT=DYNAMIC
  COMMENT='Arborescence des catégories (auto-référente)';

CREATE TABLE `Client` (
    `id` INT(10) UNSIGNED ZEROFILL NOT NULL AUTO_INCREMENT,
    `Email` VARCHAR(190) NOT NULL,
    `Nom` VARCHAR(100) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT '',
    `Prenom` VARCHAR(100) NOT NULL DEFAULT '' COMMENT 'Prénom',
    `Ville_Balte` VARCHAR(60) CHARACTER SET latin7 COLLATE latin7_general_ci DEFAULT NULL,
    `Pseudo` VARCHAR(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci DEFAULT NULL
        COMMENT 'peut contenir des émojis',
    `Newsletter` TINYINT(1) NOT NULL DEFAULT '0',
    `Civilite` ENUM('M','Mme','Mlle') DEFAULT NULL,
    `Preferences` SET('email','sms','courrier') NOT NULL DEFAULT '',
    `Date_Naissance` DATE NOT NULL DEFAULT '0000-00-00',
    `Annee_Permis` YEAR DEFAULT NULL,
    `Date_Inscription` DATETIME NOT NULL DEFAULT '0000-00-00 00:00:00',
    `Date_Maj` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uniq_Email` (`Email`),
    KEY `idx_Nom_Prenom` (`Nom`,`Prenom`),
    KEY `idx_Email_Debut` (`Email`(10))
) ENGINE=InnoDB DEFAULT CHARSET=utf8 COLLATE=utf8_unicode_ci ROW_FORMAT=COMPACT
  COMMENT='Clients (données personnelles)';

CREATE TABLE `Commande` (
    `id` INT(10) UNSIGNED NOT NULL AUTO_INCREMENT,
    `Client_id` INT(10) UNSIGNED ZEROFILL NOT NULL,
    `Statut_id` TINYINT(3) UNSIGNED NOT NULL DEFAULT '1',
    `Reference` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    `Montant_HT` DECIMAL(10,2) NOT NULL DEFAULT '0.00',
    `Remise` FLOAT UNSIGNED NOT NULL DEFAULT '0',
    `Poids_Kg` FLOAT(7,3) DEFAULT NULL,
    `Commentaire` TEXT COMMENT 'saisi par l''internaute',
    `Note_Interne` VARCHAR(255) CHARACTER SET latin1 NOT NULL DEFAULT '',
    `Nb_Modifs` INT(11) NOT NULL DEFAULT '0',
    `Nb_Insert` INT(11) NOT NULL DEFAULT '0' COMMENT 'compté par trg_Commande_bi_compteur',
    `Date_Commande` DATETIME NOT NULL DEFAULT '0000-00-00 00:00:00',
    `Date_Creation` TIMESTAMP NOT NULL DEFAULT '0000-00-00 00:00:00',
    `Date_Maj` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    `Date_Expedition` TIMESTAMP NULL DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uniq_Reference` (`Reference`),
    KEY `idx_Client_Date` (`Client_id`,`Date_Commande`),
    KEY `idx_Statut` (`Statut_id`),
    FULLTEXT KEY `ft_Commentaire` (`Commentaire`),
    CONSTRAINT `fk_Commande_Client` FOREIGN KEY (`Client_id`) REFERENCES `Client` (`id`)
        ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT `fk_Commande_Statut` FOREIGN KEY (`Statut_id`) REFERENCES `Statut_Commande` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci ROW_FORMAT=DYNAMIC
  COMMENT='Commandes passées sur le site';

-- No primary key, duplicate rows allowed.
CREATE TABLE `Ligne_Commande` (
    `Commande_id` INT(10) UNSIGNED NOT NULL,
    `Categorie_id` INT(10) UNSIGNED DEFAULT NULL,
    `Article` VARCHAR(100) NOT NULL DEFAULT '',
    `Quantite` SMALLINT(5) UNSIGNED NOT NULL DEFAULT '1',
    `Prix_Unitaire` DECIMAL(10,2) NOT NULL DEFAULT '0.00',
    `Code_Barre` BINARY(8) DEFAULT NULL,
    KEY `idx_Commande` (`Commande_id`),
    KEY `idx_Categorie` (`Categorie_id`),
    CONSTRAINT `fk_Ligne_Commande` FOREIGN KEY (`Commande_id`) REFERENCES `Commande` (`id`)
        ON DELETE CASCADE,
    CONSTRAINT `fk_Ligne_Categorie` FOREIGN KEY (`Categorie_id`) REFERENCES `Categorie` (`id`)
        ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8 COLLATE=utf8_bin ROW_FORMAT=COMPACT;

CREATE TABLE `Produit_Fiche` (
    `Ref` VARCHAR(20) CHARACTER SET ascii NOT NULL,
    `Titre` VARCHAR(255) NOT NULL,
    `Fiche` JSON DEFAULT NULL,
    `Photo` MEDIUMBLOB,
    `Empreinte` VARBINARY(16) NOT NULL DEFAULT '',
    `Etat` ENUM('neuf','occasion','rechapé') NOT NULL DEFAULT 'neuf',
    `Prix` DECIMAL(12,4) UNSIGNED DEFAULT NULL,
    `Stock` INT(11) NOT NULL DEFAULT '-1',
    `Date_Maj` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`Ref`),
    KEY `idx_Titre_Debut` (`Titre`(10)),
    FULLTEXT KEY `ft_Titre` (`Titre`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Fiche produit : caractéristiques « JSON »';

-- MyISAM, no primary key, TIMESTAMP columns without any clause: under
-- explicit_defaults_for_timestamp=OFF the first one becomes NOT NULL DEFAULT CURRENT_TIMESTAMP
-- ON UPDATE CURRENT_TIMESTAMP, the second NOT NULL DEFAULT '0000-00-00 00:00:00'.
CREATE TABLE `Journal_MyISAM` (
    `Commande_id` INT(10) UNSIGNED NOT NULL DEFAULT '0',
    `Message` VARCHAR(255) NOT NULL,
    `Detail` TEXT,
    `Horodatage` TIMESTAMP,
    `Horodatage_Fin` TIMESTAMP,
    KEY `idx_Commande` (`Commande_id`),
    FULLTEXT KEY `ft_Message_Detail` (`Message`,`Detail`)
) ENGINE=MyISAM DEFAULT CHARSET=latin1 COMMENT='Journal d''activité (MyISAM)';

CREATE TABLE `Compteur_Fixe` (
    `Cle` CHAR(20) NOT NULL,
    `Jour` DATE NOT NULL DEFAULT '0000-00-00',
    `Valeur` INT(10) UNSIGNED ZEROFILL NOT NULL DEFAULT '0000000000',
    `Ratio` DOUBLE NOT NULL DEFAULT '0',
    PRIMARY KEY (`Cle`,`Jour`)
) ENGINE=MyISAM DEFAULT CHARSET=latin1 COLLATE=latin1_swedish_ci ROW_FORMAT=FIXED;

-- An InnoDB table COMPACT without a ROW_FORMAT clause, as the production tables created before
-- the default became DYNAMIC (MySQL 5.7.9; allopneus.tb_client_activite...): SHOW CREATE TABLE
-- does not show its row format, information_schema.TABLES does. Created with that default for
-- the time of the statement, the server given back the default of all the servers here.
SET GLOBAL innodb_default_row_format = 'compact';
CREATE TABLE `Client_Activite` (
    `id` INT(11) NOT NULL AUTO_INCREMENT,
    `Libelle` VARCHAR(100) NOT NULL DEFAULT '',
    `Detail` TEXT,
    PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=latin1;
SET GLOBAL innodb_default_row_format = 'dynamic';

-- Triggers: two BEFORE INSERT on Commande in a set order (the second FOLLOWS the first), a
-- BEFORE UPDATE on Commande, and (at the end of the file) a BEFORE INSERT on Client.
-- trg_Commande_bi_compteur is not idempotent: a row copied while it is active gets
-- Nb_Insert + 1.
--
-- Bodies and routines are written BEGIN ... END: a single-statement body followed by ";" in a
-- multi-statement string is stored with that ";" by MySQL 5.7, which mysqldump then cannot
-- reload (the production triggers, created with the mysql client and DELIMITER, have none).
-- The only single-statement body is the last statement of the file, without ";".
CREATE TRIGGER `trg_Commande_bi_reference` BEFORE INSERT ON `Commande` FOR EACH ROW
BEGIN
    SET NEW.`Reference` = UPPER(NEW.`Reference`);
END;

CREATE TRIGGER `trg_Commande_bi_compteur` BEFORE INSERT ON `Commande` FOR EACH ROW
FOLLOWS `trg_Commande_bi_reference`
BEGIN
    -- compte les insertions ; la note rappelle l'ordre : « référence » d'abord
    SET NEW.`Nb_Insert` = NEW.`Nb_Insert` + 1;
    IF NEW.`Note_Interne` = '' THEN
        SET NEW.`Note_Interne` = CONCAT('créée : ', CONVERT(NEW.`Reference` USING utf8mb4));
    END IF;
END;

CREATE TRIGGER `trg_Commande_bu` BEFORE UPDATE ON `Commande` FOR EACH ROW
BEGIN
    IF NEW.`Montant_HT` <> OLD.`Montant_HT` THEN
        SET NEW.`Nb_Modifs` = OLD.`Nb_Modifs` + 1;
    END IF;
END;

CREATE FUNCTION `Prix_TTC`(`ht` DECIMAL(10,2)) RETURNS DECIMAL(10,2)
    DETERMINISTIC NO SQL
    COMMENT 'Prix TTC (TVA à 20 %)'
BEGIN
    RETURN ROUND(`ht` * 1.20, 2);
END;

CREATE FUNCTION `Nom_Complet`(`nom` VARCHAR(100), `prenom` VARCHAR(100)) RETURNS VARCHAR(201) CHARSET utf8
    DETERMINISTIC NO SQL
    COMMENT 'Prénom puis nom, séparés d''une espace'
BEGIN
    RETURN CONCAT_WS(' ', `prenom`, `nom`);
END;

INSERT INTO `Statut_Commande` (`id`, `Code`, `Libelle`, `Ordre`) VALUES
    (1, 'CRE', 'Créée', 1),
    (2, 'EXP', 'Expédiée', 2),
    (3, 'LIV', 'Livrée à l''adresse', 3),
    (4, 'ANN', '', 0);

INSERT INTO `Categorie` (`id`, `Parent_id`, `Nom`, `Slug`, `Description`) VALUES
    (1, NULL, 'Pneus', 'pneus', 'Tous les pneus'),
    (2, 1, 'Pneus été', 'pneus-ete', NULL),
    (3, 1, 'Pneus hiver « neige »', 'pneus-hiver', ''),
    (4, 2, 'Œuvres — 4×4', 'oeuvres-4x4', 'utf8 sur 3 octets : € ✓ ß');

INSERT INTO `Client`
    (`id`, `Email`, `Nom`, `Prenom`, `Ville_Balte`, `Pseudo`, `Newsletter`, `Civilite`, `Preferences`,
     `Date_Naissance`, `Annee_Permis`, `Date_Inscription`, `Date_Maj`)
VALUES
    (1, ' Helene@Example.COM ', 'Gâteau', 'Hélène', 'Šiaulių', '🚗💨 pneu', 1, 'Mme', 'email,sms',
     '1980-02-29', 1998, '2010-05-06 07:08:09', '2020-01-02 03:04:05'),
    (2, 'francois@example.com', 'Œuvré', 'François', NULL, '', 0, 'M', '',
     '0000-00-00', NULL, '0000-00-00 00:00:00', '2021-06-07 08:09:10'),
    (3, 'zoe@example.com', 'Noël', 'Zoë', 'Rīga', NULL, 0, NULL, 'courrier',
     '1975-12-31', 2000, '2022-12-20 09:30:00', '2022-12-24 23:59:59'),
    (4, 'octets@example.com', CONCAT('L', _latin1 X'E98C80819D'), '', '', '€', 0, 'Mlle', 'sms,courrier',
     '2001-01-01', 1901, '2001-01-01 00:00:00', '2001-01-01 00:00:01');

INSERT INTO `Commande`
    (`id`, `Client_id`, `Statut_id`, `Reference`, `Montant_HT`, `Remise`, `Poids_Kg`, `Commentaire`,
     `Note_Interne`, `Nb_Modifs`, `Date_Commande`, `Date_Creation`, `Date_Maj`, `Date_Expedition`)
VALUES
    (1, 1, 1, 'cmd-0001', '12.50', 0, 2.5, 'première commande 🚗 avec l''émoji', 'note é', 0,
     '2020-01-02 03:04:05', '0000-00-00 00:00:00', '2020-01-02 03:04:05', NULL),
    (2, 2, 2, 'CMD-0002', '0.00', 0.1, NULL, NULL, '', 3,
     '0000-00-00 00:00:00', '2021-06-01 10:00:00', '2021-06-07 08:09:10', '2021-06-02 11:00:00'),
    (3, 3, 3, 'cmd-0003', '199.99', 0.0015, 1234.567, '', 'Côte-d''Or', 0,
     '2022-12-20 09:30:00', '2022-12-20 09:30:00', '2022-12-24 23:59:59', '0000-00-00 00:00:00'),
    (4, 1, 4, 'CMD-0004', '-5.00', 3.4028e38, -0.001, 'Œ € « » ‰ ™', '', 1,
     '1970-01-01 00:00:01', '1970-01-01 00:00:01', '2038-01-19 03:14:07', NULL);

INSERT INTO `Ligne_Commande` (`Commande_id`, `Categorie_id`, `Article`, `Quantite`, `Prix_Unitaire`, `Code_Barre`) VALUES
    (1, 2, 'Pneu été 205/55 R16', 4, '89.90', X'0123456789ABCDEF'),
    (1, 2, 'Pneu été 205/55 R16', 4, '89.90', X'0123456789ABCDEF'),
    (1, NULL, 'Valve à clipser', 4, '2.50', NULL),
    (2, 3, 'Pneu hiver « neige »', 2, '120.00', X'00000000000000FF'),
    (3, NULL, '', 1, '0.00', X'FF00FF00FF00FF00'),
    (4, 4, 'Montage Côte-d''Or', 65535, '0.01', X'2000');

INSERT INTO `Produit_Fiche` (`Ref`, `Titre`, `Fiche`, `Photo`, `Empreinte`, `Etat`, `Prix`, `Stock`, `Date_Maj`) VALUES
    ('P-001', 'Pneu Michelin Primacy 4 205/55 R16 91V',
     '{"marque": "Michelin", "saison": "été", "emoji": "🚗", "dims": [205, 55, 16], "runflat": false}',
     X'89504E470D0A1A0A0000001A', X'00FF7F80', 'neuf', '89.9000', 12, '2020-01-02 03:04:05'),
    ('P-002', 'Pneu rechapé « hiver »', NULL, NULL, '', 'rechapé', NULL, -1, '0000-00-00 00:00:00'),
    ('P-003', 'Œuvre d''art 🚗', '{}', '', X'00', 'occasion', '0.0000', 0, '2021-06-07 08:09:10');

INSERT INTO `Journal_MyISAM` (`Commande_id`, `Message`, `Detail`, `Horodatage`, `Horodatage_Fin`) VALUES
    (1, 'commande créée', 'détail é', '2020-01-02 03:04:05', '0000-00-00 00:00:00'),
    (2, 'colis expédié', NULL, '0000-00-00 00:00:00', '2021-06-02 11:00:00'),
    (2, 'colis expédié', NULL, '0000-00-00 00:00:00', '2021-06-02 11:00:00'),
    (3, '', '', '2022-12-24 18:00:00', '2022-12-24 18:00:01'),
    (4, 'Œuvre € livrée à l''accueil', 'cp1252 : ‰ ™ …', '2001-01-01 00:00:00', '0000-00-00 00:00:00');

INSERT INTO `Client_Activite` (`id`, `Libelle`, `Detail`) VALUES
    (1, 'Particulier', NULL),
    (2, 'Garage « agréé »', 'détail é');

INSERT INTO `Compteur_Fixe` (`Cle`, `Jour`, `Valeur`, `Ratio`) VALUES
    ('visites', '2020-01-01', 42, 0.1),
    ('visites', '0000-00-00', 0, -1.5e300),
    ('ventes €', '2021-06-07', 4294967295, 2.2250738585072014e-308);

-- Created after the rows, as a trigger added to a table that already holds data: the first
-- client keeps its e-mail address as it was written (spaces and capitals). A destination that
-- fires it while copying the rows changes that address.
CREATE TRIGGER `trg_Client_bi` BEFORE INSERT ON `Client` FOR EACH ROW
SET NEW.`Email` = LOWER(TRIM(NEW.`Email`))
