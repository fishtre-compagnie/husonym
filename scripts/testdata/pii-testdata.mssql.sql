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


IF OBJECT_ID('dbo.clients','U') IS NOT NULL DROP TABLE dbo.clients;
IF OBJECT_ID('dbo.donnees_brutes','U') IS NOT NULL DROP TABLE dbo.donnees_brutes;

CREATE TABLE dbo.clients (
  id INT NOT NULL PRIMARY KEY,
  civilite NVARCHAR(100),
  prenom NVARCHAR(100),
  nom NVARCHAR(100),
  nom_complet NVARCHAR(100),
  email NVARCHAR(100),
  telephone NVARCHAR(100),
  telephone_intl NVARCHAR(100),
  date_naissance DATE,
  date_naissance_txt_fr NVARCHAR(100),
  date_naissance_txt_us NVARCHAR(100),
  date_naissance_txt_us_slash NVARCHAR(100),
  date_naissance_ambigue NVARCHAR(100),
  date_naissance_texte NVARCHAR(100),
  adresse NVARCHAR(100),
  code_postal NVARCHAR(100),
  ville NVARCHAR(100),
  numero_secu NVARCHAR(100),
  iban NVARCHAR(100),
  siret NVARCHAR(100),
  carte_bancaire NVARCHAR(100),
  adresse_ip NVARCHAR(100),
  salaire_annuel INT
);

INSERT INTO dbo.clients (id, civilite, prenom, nom, nom_complet, email, telephone, telephone_intl, date_naissance, date_naissance_txt_fr, date_naissance_txt_us, date_naissance_txt_us_slash, date_naissance_ambigue, date_naissance_texte, adresse, code_postal, ville, numero_secu, iban, siret, carte_bancaire, adresse_ip, salaire_annuel) VALUES
  (1, N'M.', N'Jean', N'Dupont', N'Jean Dupont', N'jean.dupont@example.fr', N'0710203040', N'+33 7 10 20 30 40', N'1980-12-25', N'25/12/1980', N'1980-12-25', N'12/25/1980', N'03/04/1985', N'25 decembre 1980', N'12 rue Sainte-Catherine', N'33000', N'Bordeaux', N'180017511600146', N'FR7630006000011234567890189', N'44320098000011', N'4970100123456788', N'192.168.1.10', 28000),
  (2, N'Mme', N'Marie', N'Martin', N'Marie Martin', N'marie.martin@example.fr', N'0611233745', N'+33 6 11 23 37 45', N'1975-03-08', N'08/03/1975', N'1975-03-08', N'03/08/1975', N'05/06/1990', N'8 mars 1975', N'45 rue de la Republique', N'69002', N'Lyon', N'275126938800281', N'FR1420041010050500013M02606', N'56200515000023', N'5555123400001234', N'192.168.2.11', 29450),
  (3, N'M.', N'Pierre', N'Bernard', N'Pierre Bernard', N'pierre.bernard@example.fr', N'0712264450', N'+33 7 12 26 44 50', N'1992-07-14', N'14/07/1992', N'1992-07-14', N'07/14/1992', N'11/02/1978', N'14 juillet 1992', N'8 place du Capitole', N'31000', N'Toulouse', N'165083305500471', N'FR8810278073000002056360189', N'39150672000039', N'4000000123456784', N'192.168.3.12', 30900),
  (4, N'Mme', N'Sophie', N'Dubois', N'Sophie Dubois', N'sophie.dubois@example.fr', N'0613295155', N'+33 6 13 29 51 55', N'1988-01-30', N'30/01/1988', N'1988-01-30', N'01/30/1988', N'07/09/1995', N'30 janvier 1988', N'23 rue Crebillon', N'44000', N'Nantes', NULL, N'FR7630006000011234567890189', N'44320098000011', N'4970100123456788', N'192.168.4.13', 32350),
  (5, N'M.', N'Luc', N'Thomas', N'Luc Thomas', N'luc.thomas@example.fr', N'0714325860', N'+33 7 14 32 58 60', N'1965-09-02', N'02/09/1965', N'1965-09-02', N'09/02/1965', N'02/12/1982', N'2 septembre 1965', N'5 rue des Grandes Arcades', N'67000', N'Strasbourg', N'177021234500679', N'FR1420041010050500013M02606', N'56200515000023', NULL, N'192.168.5.14', 33800),
  (6, N'Mme', N'Camille', N'Robert', N'Camille Robert', N'camille.robert@example.fr', N'0615356565', N'+33 6 15 35 65 65', N'1997-11-19', N'19/11/1997', N'1997-11-19', N'11/19/1997', N'09/03/1988', N'19 novembre 1997', N'17 rue Faidherbe', N'59000', N'Lille', N'288114567800902', N'FR8810278073000002056360189', N'39150672000039', N'4000000123456784', N'192.168.1.15', 35250),
  (7, N'M.', N'Nicolas', N'Petit', N'Nicolas Petit', N'nicolas.petit@example.fr', N'0716387270', N'+33 7 16 38 72 70', N'1983-05-21', N'21/05/1983', N'1983-05-21', N'05/21/1983', N'12/07/1991', N'21 mai 1983', N'3 rue de la Loge', N'34000', N'Montpellier', N'180017511600146', NULL, N'44320098000011', N'4970100123456788', N'192.168.2.16', 36700),
  (8, N'Mme', N'Julie', N'Durand', N'Julie Durand', N'julie.durand@example.fr', N'0617417975', N'+33 6 17 41 79 75', N'1990-02-06', N'06/02/1990', N'1990-02-06', N'02/06/1990', N'04/11/1986', N'6 fevrier 1990', N'31 rue Le Bastard', N'35000', N'Rennes', NULL, N'FR1420041010050500013M02606', N'56200515000023', N'5555123400001234', N'192.168.3.17', 38150),
  (9, N'M.', N'Thomas', N'Leroy', N'Thomas Leroy', N'thomas.leroy@example.fr', N'0718448680', N'+33 7 18 44 86 80', N'1978-08-11', N'11/08/1978', N'1978-08-11', N'08/11/1978', N'06/01/1993', N'11 aout 1978', N'9 place Drouet d''Erlon', N'51100', N'Reims', N'165083305500471', N'FR8810278073000002056360189', N'39150672000039', N'4000000123456784', N'192.168.4.18', 39600),
  (10, N'Mme', N'Emilie', N'Moreau', N'Emilie Moreau', N'emilie.moreau@example.fr', N'0619479385', N'+33 6 19 47 93 85', N'1995-04-27', N'27/04/1995', N'1995-04-27', N'04/27/1995', N'10/05/1980', N'27 avril 1995', N'14 rue de Paris', N'76600', N'Le Havre', N'292066401900374', N'FR7630006000011234567890189', N'44320098000011', NULL, N'192.168.5.19', 41050),
  (11, N'M.', N'Antoine', N'Simon', N'Antoine Simon', N'antoine.simon@example.fr', N'0720500090', N'+33 7 20 50 00 90', N'1971-06-03', N'03/06/1971', N'1971-06-03', N'06/03/1971', N'01/08/1977', N'3 juin 1971', N'22 rue des Martyrs', N'42000', N'Saint-Etienne', N'177021234500679', N'FR1420041010050500013M02606', N'56200515000023', N'5555123400001234', N'192.168.1.20', 42500),
  (12, N'Mme', N'Chloe', N'Laurent', N'Chloe Laurent', N'chloe.laurent@example.fr', N'0621530795', N'+33 6 21 53 07 95', N'2000-10-15', N'15/10/2000', N'2000-10-15', N'10/15/2000', N'08/04/1999', N'15 octobre 2000', N'6 rue Jean Jaures', N'83000', N'Toulon', NULL, N'FR8810278073000002056360189', N'39150672000039', N'4000000123456784', N'192.168.2.21', 43950),
  (13, N'M.', N'Mathieu', N'Lefebvre', N'Mathieu Lefebvre', N'mathieu.lefebvre@example.fr', N'0722561400', N'+33 7 22 56 14 00', N'1986-12-09', N'09/12/1986', N'1986-12-09', N'12/09/1986', N'03/10/1984', N'9 decembre 1986', N'40 cours Berriat', N'38000', N'Grenoble', N'180017511600146', N'FR7630006000011234567890189', N'44320098000011', N'4970100123456788', N'192.168.3.22', 45400),
  (14, N'Mme', N'Laura', N'Michel', N'Laura Michel', N'laura.michel@example.fr', N'0623592105', N'+33 6 23 59 21 05', N'1993-03-22', N'22/03/1993', N'1993-03-22', N'03/22/1993', N'12/02/1996', N'22 mars 1993', N'11 rue de la Liberte', N'21000', N'Dijon', N'275126938800281', NULL, N'56200515000023', N'5555123400001234', N'192.168.4.23', 46850),
  (15, N'M.', N'Guillaume', N'Garcia', N'Guillaume Garcia', N'guillaume.garcia@example.fr', N'0724622810', N'+33 7 24 62 28 10', N'1969-01-17', N'17/01/1969', N'1969-01-17', N'01/17/1969', N'05/07/1974', N'17 janvier 1969', N'7 rue Saint-Laud', N'49000', N'Angers', N'165083305500471', N'FR8810278073000002056360189', N'39150672000039', NULL, N'192.168.5.24', 48300),
  (16, N'Mme', N'Manon', N'David', N'Manon David', N'manon.david@example.fr', N'0625653515', N'+33 6 25 65 35 15', N'1998-07-05', N'05/07/1998', N'1998-07-05', N'07/05/1998', N'02/09/1989', N'5 juillet 1998', N'19 boulevard Victor Hugo', N'30000', N'Nimes', NULL, N'FR7630006000011234567890189', N'44320098000011', N'4970100123456788', N'192.168.1.25', 49750),
  (17, N'M.', N'Alexandre', N'Bertrand', N'Alexandre Bertrand', N'alexandre.bertrand@example.fr', N'0726684220', N'+33 7 26 68 42 20', N'1982-11-28', N'28/11/1982', N'1982-11-28', N'11/28/1982', N'11/06/1981', N'28 novembre 1982', N'28 rue du 4 Aout 1789', N'69100', N'Villeurbanne', N'177021234500679', N'FR1420041010050500013M02606', N'56200515000023', N'5555123400001234', N'192.168.2.26', 51200),
  (18, N'Mme', N'Sarah', N'Roux', N'Sarah Roux', N'sarah.roux@example.fr', N'0627714925', N'+33 6 27 71 49 25', N'1991-09-13', N'13/09/1991', N'1991-09-13', N'09/13/1991', N'07/12/1994', N'13 septembre 1991', N'4 rue des Gras', N'63000', N'Clermont-Ferrand', N'288114567800902', N'FR8810278073000002056360189', N'39150672000039', N'4000000123456784', N'192.168.3.27', 52650),
  (19, N'M.', N'Julien', N'Vincent', N'Julien Vincent', N'julien.vincent@example.fr', N'0728745630', N'+33 7 28 74 56 30', N'1974-05-01', N'01/05/1974', N'1974-05-01', N'05/01/1974', N'09/01/1972', N'1 mai 1974', N'16 cours Mirabeau', N'13100', N'Aix-en-Provence', N'180017511600146', N'FR7630006000011234567890189', N'44320098000011', N'4970100123456788', N'192.168.4.28', 54100),
  (20, N'Mme', N'Lea', N'Fournier', N'Lea Fournier', N'lea.fournier@example.fr', N'0629776335', N'+33 6 29 77 63 35', N'2001-02-24', N'24/02/2001', N'2001-02-24', N'02/24/2001', N'04/03/2002', N'24 fevrier 2001', N'33 rue de Siam', N'29200', N'Brest', NULL, N'FR1420041010050500013M02606', N'56200515000023', NULL, N'192.168.5.29', 55550),
  (21, N'M.', N'Maxime', N'Morel', N'Maxime Morel', N'maxime.morel@example.fr', N'0730807040', N'+33 7 30 80 70 40', N'1987-10-07', N'07/10/1987', N'1987-10-07', N'10/07/1987', N'06/11/1985', N'7 octobre 1987', N'10 rue du Clocher', N'87000', N'Limoges', N'165083305500471', NULL, N'39150672000039', N'4000000123456784', N'192.168.1.30', 57000),
  (22, N'Mme', N'Ines', N'Girard', N'Ines Girard', N'ines.girard@example.fr', N'0631837745', N'+33 6 31 83 77 45', N'1996-06-18', N'18/06/1996', N'1996-06-18', N'06/18/1996', N'01/05/1998', N'18 juin 1996', N'25 rue Nationale', N'37000', N'Tours', N'292066401900374', N'FR7630006000011234567890189', N'44320098000011', N'4970100123456788', N'192.168.2.31', 58450),
  (23, N'M.', N'Hugo', N'Andre', N'Hugo Andre', N'hugo.andre@example.fr', N'0732868450', N'+33 7 32 86 84 50', N'1979-04-04', N'04/04/1979', N'1979-04-04', N'04/04/1979', N'10/08/1976', N'4 avril 1979', N'2 rue des Trois Cailloux', N'80000', N'Amiens', N'177021234500679', N'FR1420041010050500013M02606', N'56200515000023', N'5555123400001234', N'192.168.3.32', 59900),
  (24, N'Mme', N'Clara', N'Mercier', N'Clara Mercier', N'clara.mercier@example.fr', N'0633899155', N'+33 6 33 89 91 55', N'1994-08-31', N'31/08/1994', N'1994-08-31', N'08/31/1994', N'12/04/1992', N'31 aout 1994', N'13 rue Serpenoise', N'57000', N'Metz', NULL, N'FR8810278073000002056360189', N'39150672000039', N'4000000123456784', N'192.168.4.33', 61350);

CREATE TABLE dbo.donnees_brutes (
  id INT NOT NULL PRIMARY KEY,
  col_1 NVARCHAR(100),
  col_2 NVARCHAR(100),
  col_3 NVARCHAR(100),
  col_4 NVARCHAR(100),
  col_5 NVARCHAR(100),
  col_6 NVARCHAR(100),
  col_7 NVARCHAR(100),
  col_8 NVARCHAR(100),
  col_9 NVARCHAR(100),
  champ_libre NVARCHAR(300)
);

INSERT INTO dbo.donnees_brutes (id, col_1, col_2, col_3, col_4, col_5, col_6, col_7, col_8, col_9, champ_libre) VALUES
  (1, N'jean.dupont@example.fr', N'0710203040', N'Jean Dupont', N'Bordeaux', N'FR7630006000011234567890189', N'25/12/1980', N'1980-12-25', N'192.168.1.10', N'180017511600146', N'Client Jean Dupont joignable au 0710203040, ne le 25/12/1980 a Bordeaux.'),
  (2, N'marie.martin@example.fr', N'0611233745', N'Marie Martin', N'Lyon', N'FR1420041010050500013M02606', N'08/03/1975', N'1975-03-08', N'192.168.2.11', N'275126938800281', N'Client Marie Martin joignable au 0611233745, ne le 08/03/1975 a Lyon.'),
  (3, N'pierre.bernard@example.fr', N'0712264450', N'Pierre Bernard', N'Toulouse', N'FR8810278073000002056360189', N'14/07/1992', N'1992-07-14', N'192.168.3.12', N'165083305500471', N'Client Pierre Bernard joignable au 0712264450, ne le 14/07/1992 a Toulouse.'),
  (4, N'sophie.dubois@example.fr', N'0613295155', N'Sophie Dubois', N'Nantes', N'FR7630006000011234567890189', N'30/01/1988', N'1988-01-30', N'192.168.4.13', NULL, N'Client Sophie Dubois joignable au 0613295155, ne le 30/01/1988 a Nantes.'),
  (5, N'luc.thomas@example.fr', N'0714325860', N'Luc Thomas', N'Strasbourg', N'FR1420041010050500013M02606', N'02/09/1965', N'1965-09-02', N'192.168.5.14', N'177021234500679', N'Client Luc Thomas joignable au 0714325860, ne le 02/09/1965 a Strasbourg.'),
  (6, N'camille.robert@example.fr', N'0615356565', N'Camille Robert', N'Lille', N'FR8810278073000002056360189', N'19/11/1997', N'1997-11-19', N'192.168.1.15', N'288114567800902', N'Client Camille Robert joignable au 0615356565, ne le 19/11/1997 a Lille.'),
  (7, N'nicolas.petit@example.fr', N'0716387270', N'Nicolas Petit', N'Montpellier', NULL, N'21/05/1983', N'1983-05-21', N'192.168.2.16', N'180017511600146', N'Client Nicolas Petit joignable au 0716387270, ne le 21/05/1983 a Montpellier.'),
  (8, N'julie.durand@example.fr', N'0617417975', N'Julie Durand', N'Rennes', N'FR1420041010050500013M02606', N'06/02/1990', N'1990-02-06', N'192.168.3.17', NULL, N'Client Julie Durand joignable au 0617417975, ne le 06/02/1990 a Rennes.'),
  (9, N'thomas.leroy@example.fr', N'0718448680', N'Thomas Leroy', N'Reims', N'FR8810278073000002056360189', N'11/08/1978', N'1978-08-11', N'192.168.4.18', N'165083305500471', N'Client Thomas Leroy joignable au 0718448680, ne le 11/08/1978 a Reims.'),
  (10, N'emilie.moreau@example.fr', N'0619479385', N'Emilie Moreau', N'Le Havre', N'FR7630006000011234567890189', N'27/04/1995', N'1995-04-27', N'192.168.5.19', N'292066401900374', N'Client Emilie Moreau joignable au 0619479385, ne le 27/04/1995 a Le Havre.'),
  (11, N'antoine.simon@example.fr', N'0720500090', N'Antoine Simon', N'Saint-Etienne', N'FR1420041010050500013M02606', N'03/06/1971', N'1971-06-03', N'192.168.1.20', N'177021234500679', N'Client Antoine Simon joignable au 0720500090, ne le 03/06/1971 a Saint-Etienne.'),
  (12, N'chloe.laurent@example.fr', N'0621530795', N'Chloe Laurent', N'Toulon', N'FR8810278073000002056360189', N'15/10/2000', N'2000-10-15', N'192.168.2.21', NULL, N'Client Chloe Laurent joignable au 0621530795, ne le 15/10/2000 a Toulon.'),
  (13, N'mathieu.lefebvre@example.fr', N'0722561400', N'Mathieu Lefebvre', N'Grenoble', N'FR7630006000011234567890189', N'09/12/1986', N'1986-12-09', N'192.168.3.22', N'180017511600146', N'Client Mathieu Lefebvre joignable au 0722561400, ne le 09/12/1986 a Grenoble.'),
  (14, N'laura.michel@example.fr', N'0623592105', N'Laura Michel', N'Dijon', NULL, N'22/03/1993', N'1993-03-22', N'192.168.4.23', N'275126938800281', N'Client Laura Michel joignable au 0623592105, ne le 22/03/1993 a Dijon.'),
  (15, N'guillaume.garcia@example.fr', N'0724622810', N'Guillaume Garcia', N'Angers', N'FR8810278073000002056360189', N'17/01/1969', N'1969-01-17', N'192.168.5.24', N'165083305500471', N'Client Guillaume Garcia joignable au 0724622810, ne le 17/01/1969 a Angers.'),
  (16, N'manon.david@example.fr', N'0625653515', N'Manon David', N'Nimes', N'FR7630006000011234567890189', N'05/07/1998', N'1998-07-05', N'192.168.1.25', NULL, N'Client Manon David joignable au 0625653515, ne le 05/07/1998 a Nimes.'),
  (17, N'alexandre.bertrand@example.fr', N'0726684220', N'Alexandre Bertrand', N'Villeurbanne', N'FR1420041010050500013M02606', N'28/11/1982', N'1982-11-28', N'192.168.2.26', N'177021234500679', N'Client Alexandre Bertrand joignable au 0726684220, ne le 28/11/1982 a Villeurbanne.'),
  (18, N'sarah.roux@example.fr', N'0627714925', N'Sarah Roux', N'Clermont-Ferrand', N'FR8810278073000002056360189', N'13/09/1991', N'1991-09-13', N'192.168.3.27', N'288114567800902', N'Client Sarah Roux joignable au 0627714925, ne le 13/09/1991 a Clermont-Ferrand.'),
  (19, N'julien.vincent@example.fr', N'0728745630', N'Julien Vincent', N'Aix-en-Provence', N'FR7630006000011234567890189', N'01/05/1974', N'1974-05-01', N'192.168.4.28', N'180017511600146', N'Client Julien Vincent joignable au 0728745630, ne le 01/05/1974 a Aix-en-Provence.'),
  (20, N'lea.fournier@example.fr', N'0629776335', N'Lea Fournier', N'Brest', N'FR1420041010050500013M02606', N'24/02/2001', N'2001-02-24', N'192.168.5.29', NULL, N'Client Lea Fournier joignable au 0629776335, ne le 24/02/2001 a Brest.'),
  (21, N'maxime.morel@example.fr', N'0730807040', N'Maxime Morel', N'Limoges', NULL, N'07/10/1987', N'1987-10-07', N'192.168.1.30', N'165083305500471', N'Client Maxime Morel joignable au 0730807040, ne le 07/10/1987 a Limoges.'),
  (22, N'ines.girard@example.fr', N'0631837745', N'Ines Girard', N'Tours', N'FR7630006000011234567890189', N'18/06/1996', N'1996-06-18', N'192.168.2.31', N'292066401900374', N'Client Ines Girard joignable au 0631837745, ne le 18/06/1996 a Tours.'),
  (23, N'hugo.andre@example.fr', N'0732868450', N'Hugo Andre', N'Amiens', N'FR1420041010050500013M02606', N'04/04/1979', N'1979-04-04', N'192.168.3.32', N'177021234500679', N'Client Hugo Andre joignable au 0732868450, ne le 04/04/1979 a Amiens.'),
  (24, N'clara.mercier@example.fr', N'0633899155', N'Clara Mercier', N'Metz', N'FR8810278073000002056360189', N'31/08/1994', N'1994-08-31', N'192.168.4.33', NULL, N'Client Clara Mercier joignable au 0633899155, ne le 31/08/1994 a Metz.');
