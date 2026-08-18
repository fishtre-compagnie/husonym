#!/usr/bin/env python3
"""Genere les scripts de donnees de test PII (FR/US) pour chaque base source."""
import os

OUT = "/home/vincentr/Gitclone/husonym/scripts/testdata"

# --- Jeu de donnees ---------------------------------------------------------
# 24 individus francais. Les dates sont coherentes entre les 3 representations
# (natif / texte FR / texte US) pour la MEME personne.
# date_ambigue : jour ET mois <= 12 sur TOUTES les lignes -> indecidable par
# les donnees seules, c'est le cas qui doit remonter "ambigu".
P = [
    # prenom, nom, ville, cp, rue, naissance (aaaa,mm,jj), ambigu (jj,mm,aaaa)
    ("Jean",     "Dupont",    "Bordeaux",         "33000", "12 rue Sainte-Catherine",   (1980,12,25), (3,4,1985)),
    ("Marie",    "Martin",    "Lyon",             "69002", "45 rue de la Republique",   (1975,3,8),   (5,6,1990)),
    ("Pierre",   "Bernard",   "Toulouse",         "31000", "8 place du Capitole",       (1992,7,14),  (11,2,1978)),
    ("Sophie",   "Dubois",    "Nantes",           "44000", "23 rue Crebillon",          (1988,1,30),  (7,9,1995)),
    ("Luc",      "Thomas",    "Strasbourg",       "67000", "5 rue des Grandes Arcades", (1965,9,2),   (2,12,1982)),
    ("Camille",  "Robert",    "Lille",            "59000", "17 rue Faidherbe",          (1997,11,19), (9,3,1988)),
    ("Nicolas",  "Petit",     "Montpellier",      "34000", "3 rue de la Loge",          (1983,5,21),  (12,7,1991)),
    ("Julie",    "Durand",    "Rennes",           "35000", "31 rue Le Bastard",         (1990,2,6),   (4,11,1986)),
    ("Thomas",   "Leroy",     "Reims",            "51100", "9 place Drouet d'Erlon",    (1978,8,11),  (6,1,1993)),
    ("Emilie",   "Moreau",    "Le Havre",         "76600", "14 rue de Paris",           (1995,4,27),  (10,5,1980)),
    ("Antoine",  "Simon",     "Saint-Etienne",    "42000", "22 rue des Martyrs",        (1971,6,3),   (1,8,1977)),
    ("Chloe",    "Laurent",   "Toulon",           "83000", "6 rue Jean Jaures",         (2000,10,15), (8,4,1999)),
    ("Mathieu",  "Lefebvre",  "Grenoble",         "38000", "40 cours Berriat",          (1986,12,9),  (3,10,1984)),
    ("Laura",    "Michel",    "Dijon",            "21000", "11 rue de la Liberte",      (1993,3,22),  (12,2,1996)),
    ("Guillaume","Garcia",    "Angers",           "49000", "7 rue Saint-Laud",          (1969,1,17),  (5,7,1974)),
    ("Manon",    "David",     "Nimes",            "30000", "19 boulevard Victor Hugo",  (1998,7,5),   (2,9,1989)),
    ("Alexandre","Bertrand",  "Villeurbanne",     "69100", "28 rue du 4 Aout 1789",     (1982,11,28), (11,6,1981)),
    ("Sarah",    "Roux",      "Clermont-Ferrand", "63000", "4 rue des Gras",            (1991,9,13),  (7,12,1994)),
    ("Julien",   "Vincent",   "Aix-en-Provence",  "13100", "16 cours Mirabeau",         (1974,5,1),   (9,1,1972)),
    ("Lea",      "Fournier",  "Brest",            "29200", "33 rue de Siam",            (2001,2,24),  (4,3,2002)),
    ("Maxime",   "Morel",     "Limoges",          "87000", "10 rue du Clocher",         (1987,10,7),  (6,11,1985)),
    ("Ines",     "Girard",    "Tours",            "37000", "25 rue Nationale",          (1996,6,18),  (1,5,1998)),
    ("Hugo",     "Andre",     "Amiens",           "80000", "2 rue des Trois Cailloux",  (1979,4,4),   (10,8,1976)),
    ("Clara",    "Mercier",   "Metz",             "57000", "13 rue Serpenoise",         (1994,8,31),  (12,4,1992)),
]

# Cles de controle calculees et VERIFIEES (NIR mod 97, Luhn, IBAN mod 97).
# NIR = 13 chiffres (sexe/annee/mois/dept/commune/ordre) + 2 chiffres de cle,
# cle = 97 - (numero mod 97). Les 15 chiffres doivent etre presents, sinon un
# validateur deterministe rejette la valeur.
NIRS = ["180017511600146", "275126938800281", "165083305500471",
        "292066401900374", "177021234500679", "288114567800902"]
IBANS = ["FR7630006000011234567890189", "FR1420041010050500013M02606",
         "FR8810278073000002056360189"]
SIRETS = ["44320098000011", "56200515000023", "39150672000039"]
CARDS = ["4970100123456788", "5555123400001234", "4000000123456784"]

MOIS_FR = {1:"janvier",2:"fevrier",3:"mars",4:"avril",5:"mai",6:"juin",
           7:"juillet",8:"aout",9:"septembre",10:"octobre",11:"novembre",12:"decembre"}


def slug(s):
    return (s.lower().replace(" ", "-").replace("'", "")
            .replace("é", "e").replace("è", "e").replace("ê", "e"))


def rows():
    out = []
    for i, (pre, nom, ville, cp, rue, (yy, mm, dd), (adj, amm, ayy)) in enumerate(P):
        n = i + 1
        tel = "0%d%02d%02d%02d%02d" % (6 if i % 2 else 7, (10 + i) % 100,
                                       (20 + i * 3) % 100, (30 + i * 7) % 100,
                                       (40 + i * 5) % 100)
        out.append({
            "id": n,
            "civilite": "Mme" if pre in ("Marie","Sophie","Camille","Julie","Emilie",
                                         "Chloe","Laura","Manon","Sarah","Lea",
                                         "Ines","Clara") else "M.",
            "prenom": pre,
            "nom": nom,
            "nom_complet": "%s %s" % (pre, nom),
            "email": "%s.%s@example.fr" % (slug(pre), slug(nom)),
            "telephone": tel,
            "telephone_intl": "+33 %s %s %s %s %s" % (tel[1], tel[2:4], tel[4:6],
                                                      tel[6:8], tel[8:10]),
            "naissance_iso": "%04d-%02d-%02d" % (yy, mm, dd),
            "naissance_fr": "%02d/%02d/%04d" % (dd, mm, yy),
            "naissance_us": "%04d-%02d-%02d" % (yy, mm, dd),
            "naissance_us_slash": "%02d/%02d/%04d" % (mm, dd, yy),
            "date_ambigue": "%02d/%02d/%04d" % (adj, amm, ayy),
            "date_texte_long": "%d %s %d" % (dd, MOIS_FR[mm], yy),
            "adresse": rue,
            "code_postal": cp,
            "ville": ville,
            "nir": NIRS[i % len(NIRS)] if i % 4 != 3 else None,
            # i % 7 pour les NULL : premier avec len(IBANS)=3, sinon le 3e IBAN
            # ne sortirait jamais (i % 3 == 2 tomberait toujours sur le NULL).
            "iban": IBANS[i % len(IBANS)] if i % 7 != 6 else None,
            "siret": SIRETS[i % len(SIRETS)],
            "carte": CARDS[i % len(CARDS)] if i % 5 != 4 else None,
            "ip": "192.168.%d.%d" % (i % 5 + 1, i + 10),
            "salaire": 28000 + i * 1450,
            "commentaire": "Client %s %s joignable au %s, ne le %02d/%02d/%04d a %s." % (
                pre, nom, tel, dd, mm, yy, ville),
        })
    return out


R = rows()

# --- Colonnes ---------------------------------------------------------------
# clients : noms EXPLICITES -> teste la detection par NOM (deterministe)
CLIENTS_COLS = [
    ("id", "int", "id"),
    ("civilite", "s", "civilite"),
    ("prenom", "s", "prenom"),
    ("nom", "s", "nom"),
    ("nom_complet", "s", "nom_complet"),
    ("email", "s", "email"),
    ("telephone", "s", "telephone"),
    ("telephone_intl", "s", "telephone_intl"),
    ("date_naissance", "date", "naissance_iso"),
    ("date_naissance_txt_fr", "s", "naissance_fr"),
    ("date_naissance_txt_us", "s", "naissance_us"),
    ("date_naissance_txt_us_slash", "s", "naissance_us_slash"),
    ("date_naissance_ambigue", "s", "date_ambigue"),
    ("date_naissance_texte", "s", "date_texte_long"),
    ("adresse", "s", "adresse"),
    ("code_postal", "s", "code_postal"),
    ("ville", "s", "ville"),
    ("numero_secu", "s", "nir"),
    ("iban", "s", "iban"),
    ("siret", "s", "siret"),
    ("carte_bancaire", "s", "carte"),
    ("adresse_ip", "s", "ip"),
    ("salaire_annuel", "int", "salaire"),
]

# donnees_brutes : noms OPAQUES -> seul le CONTENU peut trahir la PII (Presidio)
BRUTES_COLS = [
    ("id", "int", "id"),
    ("col_1", "s", "email"),
    ("col_2", "s", "telephone"),
    ("col_3", "s", "nom_complet"),
    ("col_4", "s", "ville"),
    ("col_5", "s", "iban"),
    ("col_6", "s", "naissance_fr"),
    ("col_7", "s", "naissance_us"),
    ("col_8", "s", "ip"),
    ("col_9", "s", "nir"),
    ("champ_libre", "s", "commentaire"),
]


def lit(kind, val, dialect):
    if val is None:
        return "NULL"
    if kind == "int":
        return str(val)
    esc = str(val).replace("'", "''")
    if dialect == "mssql":
        return "N'%s'" % esc
    return "'%s'" % esc


def sqltype(name, kind, dialect):
    if kind == "int":
        return "INT" if name != "id" else "INT"
    if kind == "date":
        return "DATE"
    n = 300 if name in ("champ_libre",) else 100
    return ("NVARCHAR(%d)" % n) if dialect == "mssql" else "VARCHAR(%d)" % n


HEADER = """-- Donnees de test PII francaises / americaines.
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
"""


def gen_sql(dialect, path):
    L = [HEADER, ""]
    if dialect == "postgres":
        L.append("DROP TABLE IF EXISTS public.clients;")
        L.append("DROP TABLE IF EXISTS public.donnees_brutes;")
        pfx, ai = "public.", "SERIAL"
    elif dialect == "mysql":
        L.append("DROP TABLE IF EXISTS clients;")
        L.append("DROP TABLE IF EXISTS donnees_brutes;")
        pfx, ai = "", "INT"
    else:
        L.append("IF OBJECT_ID('dbo.clients','U') IS NOT NULL DROP TABLE dbo.clients;")
        L.append("IF OBJECT_ID('dbo.donnees_brutes','U') IS NOT NULL DROP TABLE dbo.donnees_brutes;")
        pfx, ai = "dbo.", "INT"
    L.append("")

    for tname, cols in (("clients", CLIENTS_COLS), ("donnees_brutes", BRUTES_COLS)):
        L.append("CREATE TABLE %s%s (" % (pfx, tname))
        defs = []
        for name, kind, _ in cols:
            if name == "id":
                defs.append("  id INT NOT NULL PRIMARY KEY")
            else:
                defs.append("  %s %s" % (name, sqltype(name, kind, dialect)))
        L.append(",\n".join(defs))
        L.append(");")
        L.append("")
        colnames = ", ".join(c[0] for c in cols)
        L.append("INSERT INTO %s%s (%s) VALUES" % (pfx, tname, colnames))
        vals = []
        for r in R:
            vals.append("  (%s)" % ", ".join(
                lit(kind, r[key], dialect) for _, kind, key in cols))
        L.append(",\n".join(vals) + ";")
        L.append("")
    with open(path, "w") as f:
        f.write("\n".join(L))
    print("ecrit", path)


def gen_mongo(path):
    import json
    docs_c, docs_b = [], []
    for r in R:
        docs_c.append({name: r[key] for name, _, key in CLIENTS_COLS})
        docs_b.append({name: r[key] for name, _, key in BRUTES_COLS})
    body = """// Donnees de test PII francaises / americaines (MongoDB).
// Genere par scripts/testdata/gen-pii-testdata.py -- ne pas editer a la main.
db = db.getSiblingDB('testdb');
db.clients.drop();
db.donnees_brutes.drop();
db.clients.insertMany(%s);
db.donnees_brutes.insertMany(%s);
print('clients: ' + db.clients.countDocuments() + ' / donnees_brutes: ' + db.donnees_brutes.countDocuments());
""" % (json.dumps(docs_c, indent=2, ensure_ascii=False),
       json.dumps(docs_b, indent=2, ensure_ascii=False))
    with open(path, "w") as f:
        f.write(body)
    print("ecrit", path)


os.makedirs(OUT, exist_ok=True)
gen_sql("postgres", os.path.join(OUT, "pii-testdata.postgres.sql"))
gen_sql("mysql", os.path.join(OUT, "pii-testdata.mysql.sql"))
gen_sql("mssql", os.path.join(OUT, "pii-testdata.mssql.sql"))
gen_mongo(os.path.join(OUT, "pii-testdata.mongo.js"))
print("%d lignes par table" % len(R))
