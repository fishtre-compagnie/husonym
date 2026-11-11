-- MySQL 5.7 only: tables modelled on the legacy production databases (allopneus: 5.7.42,
-- sql_mode='', explicit_defaults_for_timestamp=OFF, latin1/utf8/utf8mb4 side by side,
-- lower_case_table_names=0). Run on a server started with tcmysql.WithMysql57:
--   * names with capitals, which a server with lower_case_table_names=0 keeps;
--   * TIMESTAMP columns with DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP, with a
--     '0000-00-00 00:00:00' default, and with no clause at all, which takes the implicit
--     defaults of explicit_defaults_for_timestamp=OFF (the first TIMESTAMP column of a table
--     DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP, the next ones NOT NULL DEFAULT
--     '0000-00-00 00:00:00');
--   * zero dates in the data, which only an empty sql_mode accepts;
--   * latin1 columns holding accented text (and a cp1252-only character, as MySQL's latin1
--     is cp1252) next to utf8 and utf8mb4 ones;
--   * a MyISAM table, and a table without a primary key;
--   * foreign keys between those tables.

CREATE TABLE IF NOT EXISTS Commande_Statut (
    id INT NOT NULL AUTO_INCREMENT,
    Libelle VARCHAR(50) NOT NULL,
    PRIMARY KEY (id)
) ENGINE = InnoDB DEFAULT CHARSET = latin1;

CREATE TABLE IF NOT EXISTS Commande (
    id INT NOT NULL AUTO_INCREMENT,
    Statut_id INT NOT NULL,
    Reference VARCHAR(32) NOT NULL,
    Client_Nom VARCHAR(100) CHARACTER SET latin1 NOT NULL DEFAULT '',
    Note TEXT CHARACTER SET utf8mb4,
    Date_Maj TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    Date_Creation TIMESTAMP NOT NULL DEFAULT '0000-00-00 00:00:00',
    -- no clause: NOT NULL DEFAULT '0000-00-00 00:00:00' under explicit_defaults_for_timestamp=OFF
    Date_Livraison TIMESTAMP,
    Date_Naissance DATE NOT NULL DEFAULT '0000-00-00',
    Montant DECIMAL(10, 2) NOT NULL DEFAULT '0.00',
    PRIMARY KEY (id),
    UNIQUE KEY uniq_Commande_Reference (Reference),
    KEY idx_Commande_Statut (Statut_id),
    CONSTRAINT fk_Commande_Statut FOREIGN KEY (Statut_id) REFERENCES Commande_Statut (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8;

-- No primary key: the lines of an order, keyed by nothing but an index
CREATE TABLE IF NOT EXISTS Ligne_Commande (
    Commande_id INT NOT NULL,
    Article VARCHAR(100) CHARACTER SET latin1 NOT NULL,
    Quantite INT NOT NULL DEFAULT 1,
    KEY idx_Ligne_Commande (Commande_id),
    CONSTRAINT fk_Ligne_Commande FOREIGN KEY (Commande_id) REFERENCES Commande (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8;

-- MyISAM, without a primary key
CREATE TABLE IF NOT EXISTS Journal_MyISAM (
    Commande_id INT NOT NULL,
    Message VARCHAR(255) NOT NULL,
    -- no clause, first TIMESTAMP column: DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
    Horodatage TIMESTAMP,
    KEY idx_Journal_Commande (Commande_id)
) ENGINE = MyISAM DEFAULT CHARSET = latin1;

INSERT INTO Commande_Statut (id, Libelle) VALUES
    (1, 'Créée'),
    (2, 'Expédiée'),
    (3, 'Livrée à l''adresse');

INSERT INTO Commande
    (id, Statut_id, Reference, Client_Nom, Note, Date_Maj, Date_Creation, Date_Livraison, Date_Naissance, Montant)
VALUES
    (1, 1, 'CMD-1', 'Hélène Gâteau', 'première commande 🚗', '2020-01-02 03:04:05', '0000-00-00 00:00:00', '0000-00-00 00:00:00', '0000-00-00', '12.50'),
    (2, 2, 'CMD-2', 'François Œuvré', NULL, '2021-06-07 08:09:10', '2021-06-01 10:00:00', '0000-00-00 00:00:00', '1980-02-29', '0.00'),
    (3, 3, 'CMD-3', 'Zoë Noël', 'livrée', '2022-12-24 23:59:59', '2022-12-20 09:30:00', '2022-12-24 18:00:00', '0000-00-00', '199.99');

INSERT INTO Ligne_Commande (Commande_id, Article, Quantite) VALUES
    (1, 'Pneu été 205/55 R16', 4),
    (1, 'Valve à clipser', 4),
    (2, 'Pneu hiver « neige »', 2),
    (3, 'Montage Côte-d''Or', 1);

INSERT INTO Journal_MyISAM (Commande_id, Message, Horodatage) VALUES
    (1, 'commande créée', '2020-01-02 03:04:05'),
    (2, 'colis expédié', '0000-00-00 00:00:00'),
    (3, 'colis livré à l''accueil', '2022-12-24 18:00:00');
