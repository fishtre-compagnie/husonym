-- Donnees de test PII francaises / americaines.
-- Genere par scripts/testdata/gen-pii-testdata.py -- ne pas editer a la main.
--
-- Deux tables, deux objectifs distincts :
--   * clients        : noms de colonnes EXPLICITES  -> teste la detection par NOM
--   * donnees_brutes : noms de colonnes OPAQUES     -> teste la detection par CONTENU
--
-- Les cles de controle (NIR mod 97, IBAN mod 97, SIRET/carte Luhn) sont VALIDES :
-- un validateur deterministe doit les accepter a 100%.
--
-- Cas de dates couverts, pour la MEME date de naissance :
--   date_naissance              type natif DATE       -> aucun format a deviner
--   date_naissance_txt_fr       texte  jj/mm/aaaa     -> tranchable (des jours > 12)
--   date_naissance_txt_us       texte  aaaa-mm-jj     -> non ambigu
--   date_naissance_txt_us_slash texte  mm/jj/aaaa     -> tranchable (des mois > 12 en pos. 2)
--   date_naissance_ambigue      texte  jj/mm ou mm/jj -> INDECIDABLE : les deux <= 12
--   date_naissance_texte        texte  "25 decembre 1980"


DROP TABLE IF EXISTS clients;
DROP TABLE IF EXISTS donnees_brutes;

CREATE TABLE clients (
  id INT NOT NULL PRIMARY KEY,
  civilite VARCHAR(100),
  prenom VARCHAR(100),
  nom VARCHAR(100),
  nom_complet VARCHAR(100),
  email VARCHAR(100),
  telephone VARCHAR(100),
  telephone_intl VARCHAR(100),
  date_naissance DATE,
  date_naissance_txt_fr VARCHAR(100),
  date_naissance_txt_us VARCHAR(100),
  date_naissance_txt_us_slash VARCHAR(100),
  date_naissance_ambigue VARCHAR(100),
  date_naissance_texte VARCHAR(100),
  adresse VARCHAR(100),
  code_postal VARCHAR(100),
  ville VARCHAR(100),
  numero_secu VARCHAR(100),
  iban VARCHAR(100),
  siret VARCHAR(100),
  carte_bancaire VARCHAR(100),
  adresse_ip VARCHAR(100),
  salaire_annuel INT
);

INSERT INTO clients (id, civilite, prenom, nom, nom_complet, email, telephone, telephone_intl, date_naissance, date_naissance_txt_fr, date_naissance_txt_us, date_naissance_txt_us_slash, date_naissance_ambigue, date_naissance_texte, adresse, code_postal, ville, numero_secu, iban, siret, carte_bancaire, adresse_ip, salaire_annuel) VALUES
  (1, 'M.', 'Jean', 'Dupont', 'Jean Dupont', 'jean.dupont@example.fr', '0710203040', '+33 7 10 20 30 40', '1980-12-25', '25/12/1980', '1980-12-25', '12/25/1980', '03/04/1985', '25 decembre 1980', '12 rue Sainte-Catherine', '33000', 'Bordeaux', '180017511600146', 'FR7630006000011234567890189', '44320098000011', '4970100123456788', '192.168.1.10', 28000),
  (2, 'Mme', 'Marie', 'Martin', 'Marie Martin', 'marie.martin@example.fr', '0611233745', '+33 6 11 23 37 45', '1975-03-08', '08/03/1975', '1975-03-08', '03/08/1975', '05/06/1990', '8 mars 1975', '45 rue de la Republique', '69002', 'Lyon', '275126938800281', 'FR1420041010050500013M02606', '56200515000023', '5555123400001234', '192.168.2.11', 29450),
  (3, 'M.', 'Pierre', 'Bernard', 'Pierre Bernard', 'pierre.bernard@example.fr', '0712264450', '+33 7 12 26 44 50', '1992-07-14', '14/07/1992', '1992-07-14', '07/14/1992', '11/02/1978', '14 juillet 1992', '8 place du Capitole', '31000', 'Toulouse', '165083305500471', 'FR8810278073000002056360189', '39150672000039', '4000000123456784', '192.168.3.12', 30900),
  (4, 'Mme', 'Sophie', 'Dubois', 'Sophie Dubois', 'sophie.dubois@example.fr', '0613295155', '+33 6 13 29 51 55', '1988-01-30', '30/01/1988', '1988-01-30', '01/30/1988', '07/09/1995', '30 janvier 1988', '23 rue Crebillon', '44000', 'Nantes', NULL, 'FR7630006000011234567890189', '44320098000011', '4970100123456788', '192.168.4.13', 32350),
  (5, 'M.', 'Luc', 'Thomas', 'Luc Thomas', 'luc.thomas@example.fr', '0714325860', '+33 7 14 32 58 60', '1965-09-02', '02/09/1965', '1965-09-02', '09/02/1965', '02/12/1982', '2 septembre 1965', '5 rue des Grandes Arcades', '67000', 'Strasbourg', '177021234500679', 'FR1420041010050500013M02606', '56200515000023', NULL, '192.168.5.14', 33800),
  (6, 'Mme', 'Camille', 'Robert', 'Camille Robert', 'camille.robert@example.fr', '0615356565', '+33 6 15 35 65 65', '1997-11-19', '19/11/1997', '1997-11-19', '11/19/1997', '09/03/1988', '19 novembre 1997', '17 rue Faidherbe', '59000', 'Lille', '288114567800902', 'FR8810278073000002056360189', '39150672000039', '4000000123456784', '192.168.1.15', 35250),
  (7, 'M.', 'Nicolas', 'Petit', 'Nicolas Petit', 'nicolas.petit@example.fr', '0716387270', '+33 7 16 38 72 70', '1983-05-21', '21/05/1983', '1983-05-21', '05/21/1983', '12/07/1991', '21 mai 1983', '3 rue de la Loge', '34000', 'Montpellier', '180017511600146', NULL, '44320098000011', '4970100123456788', '192.168.2.16', 36700),
  (8, 'Mme', 'Julie', 'Durand', 'Julie Durand', 'julie.durand@example.fr', '0617417975', '+33 6 17 41 79 75', '1990-02-06', '06/02/1990', '1990-02-06', '02/06/1990', '04/11/1986', '6 fevrier 1990', '31 rue Le Bastard', '35000', 'Rennes', NULL, 'FR1420041010050500013M02606', '56200515000023', '5555123400001234', '192.168.3.17', 38150),
  (9, 'M.', 'Thomas', 'Leroy', 'Thomas Leroy', 'thomas.leroy@example.fr', '0718448680', '+33 7 18 44 86 80', '1978-08-11', '11/08/1978', '1978-08-11', '08/11/1978', '06/01/1993', '11 aout 1978', '9 place Drouet d''Erlon', '51100', 'Reims', '165083305500471', 'FR8810278073000002056360189', '39150672000039', '4000000123456784', '192.168.4.18', 39600),
  (10, 'Mme', 'Emilie', 'Moreau', 'Emilie Moreau', 'emilie.moreau@example.fr', '0619479385', '+33 6 19 47 93 85', '1995-04-27', '27/04/1995', '1995-04-27', '04/27/1995', '10/05/1980', '27 avril 1995', '14 rue de Paris', '76600', 'Le Havre', '292066401900374', 'FR7630006000011234567890189', '44320098000011', NULL, '192.168.5.19', 41050),
  (11, 'M.', 'Antoine', 'Simon', 'Antoine Simon', 'antoine.simon@example.fr', '0720500090', '+33 7 20 50 00 90', '1971-06-03', '03/06/1971', '1971-06-03', '06/03/1971', '01/08/1977', '3 juin 1971', '22 rue des Martyrs', '42000', 'Saint-Etienne', '177021234500679', 'FR1420041010050500013M02606', '56200515000023', '5555123400001234', '192.168.1.20', 42500),
  (12, 'Mme', 'Chloe', 'Laurent', 'Chloe Laurent', 'chloe.laurent@example.fr', '0621530795', '+33 6 21 53 07 95', '2000-10-15', '15/10/2000', '2000-10-15', '10/15/2000', '08/04/1999', '15 octobre 2000', '6 rue Jean Jaures', '83000', 'Toulon', NULL, 'FR8810278073000002056360189', '39150672000039', '4000000123456784', '192.168.2.21', 43950),
  (13, 'M.', 'Mathieu', 'Lefebvre', 'Mathieu Lefebvre', 'mathieu.lefebvre@example.fr', '0722561400', '+33 7 22 56 14 00', '1986-12-09', '09/12/1986', '1986-12-09', '12/09/1986', '03/10/1984', '9 decembre 1986', '40 cours Berriat', '38000', 'Grenoble', '180017511600146', 'FR7630006000011234567890189', '44320098000011', '4970100123456788', '192.168.3.22', 45400),
  (14, 'Mme', 'Laura', 'Michel', 'Laura Michel', 'laura.michel@example.fr', '0623592105', '+33 6 23 59 21 05', '1993-03-22', '22/03/1993', '1993-03-22', '03/22/1993', '12/02/1996', '22 mars 1993', '11 rue de la Liberte', '21000', 'Dijon', '275126938800281', NULL, '56200515000023', '5555123400001234', '192.168.4.23', 46850),
  (15, 'M.', 'Guillaume', 'Garcia', 'Guillaume Garcia', 'guillaume.garcia@example.fr', '0724622810', '+33 7 24 62 28 10', '1969-01-17', '17/01/1969', '1969-01-17', '01/17/1969', '05/07/1974', '17 janvier 1969', '7 rue Saint-Laud', '49000', 'Angers', '165083305500471', 'FR8810278073000002056360189', '39150672000039', NULL, '192.168.5.24', 48300),
  (16, 'Mme', 'Manon', 'David', 'Manon David', 'manon.david@example.fr', '0625653515', '+33 6 25 65 35 15', '1998-07-05', '05/07/1998', '1998-07-05', '07/05/1998', '02/09/1989', '5 juillet 1998', '19 boulevard Victor Hugo', '30000', 'Nimes', NULL, 'FR7630006000011234567890189', '44320098000011', '4970100123456788', '192.168.1.25', 49750),
  (17, 'M.', 'Alexandre', 'Bertrand', 'Alexandre Bertrand', 'alexandre.bertrand@example.fr', '0726684220', '+33 7 26 68 42 20', '1982-11-28', '28/11/1982', '1982-11-28', '11/28/1982', '11/06/1981', '28 novembre 1982', '28 rue du 4 Aout 1789', '69100', 'Villeurbanne', '177021234500679', 'FR1420041010050500013M02606', '56200515000023', '5555123400001234', '192.168.2.26', 51200),
  (18, 'Mme', 'Sarah', 'Roux', 'Sarah Roux', 'sarah.roux@example.fr', '0627714925', '+33 6 27 71 49 25', '1991-09-13', '13/09/1991', '1991-09-13', '09/13/1991', '07/12/1994', '13 septembre 1991', '4 rue des Gras', '63000', 'Clermont-Ferrand', '288114567800902', 'FR8810278073000002056360189', '39150672000039', '4000000123456784', '192.168.3.27', 52650),
  (19, 'M.', 'Julien', 'Vincent', 'Julien Vincent', 'julien.vincent@example.fr', '0728745630', '+33 7 28 74 56 30', '1974-05-01', '01/05/1974', '1974-05-01', '05/01/1974', '09/01/1972', '1 mai 1974', '16 cours Mirabeau', '13100', 'Aix-en-Provence', '180017511600146', 'FR7630006000011234567890189', '44320098000011', '4970100123456788', '192.168.4.28', 54100),
  (20, 'Mme', 'Lea', 'Fournier', 'Lea Fournier', 'lea.fournier@example.fr', '0629776335', '+33 6 29 77 63 35', '2001-02-24', '24/02/2001', '2001-02-24', '02/24/2001', '04/03/2002', '24 fevrier 2001', '33 rue de Siam', '29200', 'Brest', NULL, 'FR1420041010050500013M02606', '56200515000023', NULL, '192.168.5.29', 55550),
  (21, 'M.', 'Maxime', 'Morel', 'Maxime Morel', 'maxime.morel@example.fr', '0730807040', '+33 7 30 80 70 40', '1987-10-07', '07/10/1987', '1987-10-07', '10/07/1987', '06/11/1985', '7 octobre 1987', '10 rue du Clocher', '87000', 'Limoges', '165083305500471', NULL, '39150672000039', '4000000123456784', '192.168.1.30', 57000),
  (22, 'Mme', 'Ines', 'Girard', 'Ines Girard', 'ines.girard@example.fr', '0631837745', '+33 6 31 83 77 45', '1996-06-18', '18/06/1996', '1996-06-18', '06/18/1996', '01/05/1998', '18 juin 1996', '25 rue Nationale', '37000', 'Tours', '292066401900374', 'FR7630006000011234567890189', '44320098000011', '4970100123456788', '192.168.2.31', 58450),
  (23, 'M.', 'Hugo', 'Andre', 'Hugo Andre', 'hugo.andre@example.fr', '0732868450', '+33 7 32 86 84 50', '1979-04-04', '04/04/1979', '1979-04-04', '04/04/1979', '10/08/1976', '4 avril 1979', '2 rue des Trois Cailloux', '80000', 'Amiens', '177021234500679', 'FR1420041010050500013M02606', '56200515000023', '5555123400001234', '192.168.3.32', 59900),
  (24, 'Mme', 'Clara', 'Mercier', 'Clara Mercier', 'clara.mercier@example.fr', '0633899155', '+33 6 33 89 91 55', '1994-08-31', '31/08/1994', '1994-08-31', '08/31/1994', '12/04/1992', '31 aout 1994', '13 rue Serpenoise', '57000', 'Metz', NULL, 'FR8810278073000002056360189', '39150672000039', '4000000123456784', '192.168.4.33', 61350);

CREATE TABLE donnees_brutes (
  id INT NOT NULL PRIMARY KEY,
  col_1 VARCHAR(100),
  col_2 VARCHAR(100),
  col_3 VARCHAR(100),
  col_4 VARCHAR(100),
  col_5 VARCHAR(100),
  col_6 VARCHAR(100),
  col_7 VARCHAR(100),
  col_8 VARCHAR(100),
  col_9 VARCHAR(100),
  champ_libre VARCHAR(300)
);

INSERT INTO donnees_brutes (id, col_1, col_2, col_3, col_4, col_5, col_6, col_7, col_8, col_9, champ_libre) VALUES
  (1, 'jean.dupont@example.fr', '0710203040', 'Jean Dupont', 'Bordeaux', 'FR7630006000011234567890189', '25/12/1980', '1980-12-25', '192.168.1.10', '180017511600146', 'Client Jean Dupont joignable au 0710203040, ne le 25/12/1980 a Bordeaux.'),
  (2, 'marie.martin@example.fr', '0611233745', 'Marie Martin', 'Lyon', 'FR1420041010050500013M02606', '08/03/1975', '1975-03-08', '192.168.2.11', '275126938800281', 'Client Marie Martin joignable au 0611233745, ne le 08/03/1975 a Lyon.'),
  (3, 'pierre.bernard@example.fr', '0712264450', 'Pierre Bernard', 'Toulouse', 'FR8810278073000002056360189', '14/07/1992', '1992-07-14', '192.168.3.12', '165083305500471', 'Client Pierre Bernard joignable au 0712264450, ne le 14/07/1992 a Toulouse.'),
  (4, 'sophie.dubois@example.fr', '0613295155', 'Sophie Dubois', 'Nantes', 'FR7630006000011234567890189', '30/01/1988', '1988-01-30', '192.168.4.13', NULL, 'Client Sophie Dubois joignable au 0613295155, ne le 30/01/1988 a Nantes.'),
  (5, 'luc.thomas@example.fr', '0714325860', 'Luc Thomas', 'Strasbourg', 'FR1420041010050500013M02606', '02/09/1965', '1965-09-02', '192.168.5.14', '177021234500679', 'Client Luc Thomas joignable au 0714325860, ne le 02/09/1965 a Strasbourg.'),
  (6, 'camille.robert@example.fr', '0615356565', 'Camille Robert', 'Lille', 'FR8810278073000002056360189', '19/11/1997', '1997-11-19', '192.168.1.15', '288114567800902', 'Client Camille Robert joignable au 0615356565, ne le 19/11/1997 a Lille.'),
  (7, 'nicolas.petit@example.fr', '0716387270', 'Nicolas Petit', 'Montpellier', NULL, '21/05/1983', '1983-05-21', '192.168.2.16', '180017511600146', 'Client Nicolas Petit joignable au 0716387270, ne le 21/05/1983 a Montpellier.'),
  (8, 'julie.durand@example.fr', '0617417975', 'Julie Durand', 'Rennes', 'FR1420041010050500013M02606', '06/02/1990', '1990-02-06', '192.168.3.17', NULL, 'Client Julie Durand joignable au 0617417975, ne le 06/02/1990 a Rennes.'),
  (9, 'thomas.leroy@example.fr', '0718448680', 'Thomas Leroy', 'Reims', 'FR8810278073000002056360189', '11/08/1978', '1978-08-11', '192.168.4.18', '165083305500471', 'Client Thomas Leroy joignable au 0718448680, ne le 11/08/1978 a Reims.'),
  (10, 'emilie.moreau@example.fr', '0619479385', 'Emilie Moreau', 'Le Havre', 'FR7630006000011234567890189', '27/04/1995', '1995-04-27', '192.168.5.19', '292066401900374', 'Client Emilie Moreau joignable au 0619479385, ne le 27/04/1995 a Le Havre.'),
  (11, 'antoine.simon@example.fr', '0720500090', 'Antoine Simon', 'Saint-Etienne', 'FR1420041010050500013M02606', '03/06/1971', '1971-06-03', '192.168.1.20', '177021234500679', 'Client Antoine Simon joignable au 0720500090, ne le 03/06/1971 a Saint-Etienne.'),
  (12, 'chloe.laurent@example.fr', '0621530795', 'Chloe Laurent', 'Toulon', 'FR8810278073000002056360189', '15/10/2000', '2000-10-15', '192.168.2.21', NULL, 'Client Chloe Laurent joignable au 0621530795, ne le 15/10/2000 a Toulon.'),
  (13, 'mathieu.lefebvre@example.fr', '0722561400', 'Mathieu Lefebvre', 'Grenoble', 'FR7630006000011234567890189', '09/12/1986', '1986-12-09', '192.168.3.22', '180017511600146', 'Client Mathieu Lefebvre joignable au 0722561400, ne le 09/12/1986 a Grenoble.'),
  (14, 'laura.michel@example.fr', '0623592105', 'Laura Michel', 'Dijon', NULL, '22/03/1993', '1993-03-22', '192.168.4.23', '275126938800281', 'Client Laura Michel joignable au 0623592105, ne le 22/03/1993 a Dijon.'),
  (15, 'guillaume.garcia@example.fr', '0724622810', 'Guillaume Garcia', 'Angers', 'FR8810278073000002056360189', '17/01/1969', '1969-01-17', '192.168.5.24', '165083305500471', 'Client Guillaume Garcia joignable au 0724622810, ne le 17/01/1969 a Angers.'),
  (16, 'manon.david@example.fr', '0625653515', 'Manon David', 'Nimes', 'FR7630006000011234567890189', '05/07/1998', '1998-07-05', '192.168.1.25', NULL, 'Client Manon David joignable au 0625653515, ne le 05/07/1998 a Nimes.'),
  (17, 'alexandre.bertrand@example.fr', '0726684220', 'Alexandre Bertrand', 'Villeurbanne', 'FR1420041010050500013M02606', '28/11/1982', '1982-11-28', '192.168.2.26', '177021234500679', 'Client Alexandre Bertrand joignable au 0726684220, ne le 28/11/1982 a Villeurbanne.'),
  (18, 'sarah.roux@example.fr', '0627714925', 'Sarah Roux', 'Clermont-Ferrand', 'FR8810278073000002056360189', '13/09/1991', '1991-09-13', '192.168.3.27', '288114567800902', 'Client Sarah Roux joignable au 0627714925, ne le 13/09/1991 a Clermont-Ferrand.'),
  (19, 'julien.vincent@example.fr', '0728745630', 'Julien Vincent', 'Aix-en-Provence', 'FR7630006000011234567890189', '01/05/1974', '1974-05-01', '192.168.4.28', '180017511600146', 'Client Julien Vincent joignable au 0728745630, ne le 01/05/1974 a Aix-en-Provence.'),
  (20, 'lea.fournier@example.fr', '0629776335', 'Lea Fournier', 'Brest', 'FR1420041010050500013M02606', '24/02/2001', '2001-02-24', '192.168.5.29', NULL, 'Client Lea Fournier joignable au 0629776335, ne le 24/02/2001 a Brest.'),
  (21, 'maxime.morel@example.fr', '0730807040', 'Maxime Morel', 'Limoges', NULL, '07/10/1987', '1987-10-07', '192.168.1.30', '165083305500471', 'Client Maxime Morel joignable au 0730807040, ne le 07/10/1987 a Limoges.'),
  (22, 'ines.girard@example.fr', '0631837745', 'Ines Girard', 'Tours', 'FR7630006000011234567890189', '18/06/1996', '1996-06-18', '192.168.2.31', '292066401900374', 'Client Ines Girard joignable au 0631837745, ne le 18/06/1996 a Tours.'),
  (23, 'hugo.andre@example.fr', '0732868450', 'Hugo Andre', 'Amiens', 'FR1420041010050500013M02606', '04/04/1979', '1979-04-04', '192.168.3.32', '177021234500679', 'Client Hugo Andre joignable au 0732868450, ne le 04/04/1979 a Amiens.'),
  (24, 'clara.mercier@example.fr', '0633899155', 'Clara Mercier', 'Metz', 'FR8810278073000002056360189', '31/08/1994', '1994-08-31', '192.168.4.33', NULL, 'Client Clara Mercier joignable au 0633899155, ne le 31/08/1994 a Metz.');
