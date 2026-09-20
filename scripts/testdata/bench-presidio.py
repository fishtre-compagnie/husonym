#!/usr/bin/env python3
"""Banc d'évaluation de Presidio sur le jeu de test PII français.

Mesure la capacité de Presidio à identifier la nature d'une colonne À PARTIR DE
SES VALEURS SEULES (le nom de colonne n'est jamais transmis). C'est bien ce qu'on
lui demande dans le produit : l'étage 3, celui qui traite les colonnes que le
dictionnaire n'a pas su nommer.

Vérité terrain : le contenu de chaque colonne est connu par construction
(cf. scripts/testdata/gen-pii-testdata.py).

Reproduit la logique de décision du backend (pii-detect.go analyzeColumn) :
analyse valeur par valeur, meilleur score par entité dans chaque valeur, puis
entité dominante = celle présente dans le plus de valeurs.
"""
import collections
import json
import subprocess
import sys
import urllib.error
import urllib.request

# --- Vérité terrain ---------------------------------------------------------
# Catégorie attendue pour chaque colonne, dans le vocabulaire de pkg/piidetect.
# None = colonne non personnelle : toute détection y est un faux positif.
TRUTH = {
    "clients": {
        "id": None,
        "civilite": "gender",
        "prenom": "person_first_name",
        "nom": "person_last_name",
        "nom_complet": "person_full_name",
        "email": "email",
        "telephone": "phone_number",
        "telephone_intl": "phone_number",
        "date_naissance": "birth_date",
        "date_naissance_txt_fr": "birth_date",
        "date_naissance_txt_us": "birth_date",
        "date_naissance_txt_us_slash": "birth_date",
        "date_naissance_ambigue": "birth_date",
        "date_naissance_texte": "birth_date",
        "adresse": "street_address",
        "code_postal": "postal_code",
        "ville": "city",
        "numero_secu": "nir",
        "iban": "iban",
        "siret": "siret",
        "carte_bancaire": "credit_card",
        "adresse_ip": "ip_address",
        "salaire_annuel": None,
    },
    "donnees_brutes": {
        "id": None,
        "col_1": "email",
        "col_2": "phone_number",
        "col_3": "person_full_name",
        "col_4": "city",
        "col_5": "iban",
        "col_6": "birth_date",
        "col_7": "birth_date",
        "col_8": "ip_address",
        "col_9": "nir",
        "champ_libre": "person_full_name",  # texte libre : un nom y est présent
    },
}

# Entité Presidio -> catégorie du produit. Reproduit SuggestionForEntity, plus
# les entités françaises de l'image FR.
ENTITY_TO_CATEGORY = {
    "EMAIL_ADDRESS": "email",
    "PHONE_NUMBER": "phone_number",
    "FR_PHONE_NUMBER": "phone_number",
    "PERSON": "person_full_name",
    "LOCATION": "city",
    "GPE": "city",
    "CREDIT_CARD": "credit_card",
    "IP_ADDRESS": "ip_address",
    "US_SSN": "nir",
    "FR_NIR": "nir",
    "IBAN_CODE": "iban",
    "FR_SIRET": "siret",
    "FR_POSTAL_CODE": "postal_code",
}

# Entités non exploitables : ignorées comme dans le backend, sinon elles
# supplanteraient l'entité utile (URL sort sur chaque email, par exemple).
IGNORED = {"URL", "NRP", "ORGANIZATION", "MEDICAL_LICENSE", "CRYPTO", "MAC_ADDRESS"}

THRESHOLD = 0.35  # même valeur que defaultScoreThresh côté backend

# Colonnes résolues AVANT Presidio dans le pipeline (validateurs à clé de contrôle
# et inférence de format de date). Elles ne lui sont jamais soumises en
# production : les inclure mesurerait autre chose que ce qu'on cherche à régler.
RESOLVED_UPSTREAM = {
    ("clients", "email"), ("clients", "telephone"), ("clients", "telephone_intl"),
    ("clients", "numero_secu"), ("clients", "iban"), ("clients", "siret"),
    ("clients", "carte_bancaire"), ("clients", "adresse_ip"), ("clients", "civilite"),
    ("clients", "date_naissance"), ("clients", "date_naissance_txt_fr"),
    ("clients", "date_naissance_txt_us"), ("clients", "date_naissance_txt_us_slash"),
    ("clients", "date_naissance_ambigue"), ("clients", "date_naissance_texte"),
    ("donnees_brutes", "col_1"), ("donnees_brutes", "col_2"),
    ("donnees_brutes", "col_5"), ("donnees_brutes", "col_8"),
    ("donnees_brutes", "col_9"),
    ("donnees_brutes", "col_6"), ("donnees_brutes", "col_7"),
}


def fetch_column(table, column):
    """Valeurs réelles d'une colonne, lues dans PostgreSQL."""
    out = subprocess.run(
        ["docker", "exec", "test-prod-db", "psql", "-U", "postgres", "-d", "postgres",
         "-Atc", f"select {column} from public.{table}"],
        capture_output=True, text=True, check=True,
    ).stdout
    return [v for v in out.split("\n") if v.strip()]


def analyze(port, text, lang):
    req = urllib.request.Request(
        f"http://localhost:{port}/analyze",
        data=json.dumps({"text": text, "language": lang,
                         "score_threshold": THRESHOLD}).encode(),
        headers={"Content-Type": "application/json"},
    )
    try:
        return json.load(urllib.request.urlopen(req, timeout=60))
    except (urllib.error.URLError, TimeoutError, json.JSONDecodeError):
        return []


def dominant_category(port, values, lang):
    """Catégorie retenue pour la colonne — même arbitrage que le backend."""
    agg = collections.defaultdict(lambda: [0, 0.0])
    analyzed = 0
    for v in values:
        if not v.strip():
            continue
        analyzed += 1
        best = {}
        for r in analyze(port, v[:200], lang):
            e = r["entity_type"]
            if e in IGNORED or e not in ENTITY_TO_CATEGORY:
                continue
            if r["score"] > best.get(e, 0):
                best[e] = r["score"]
        for e, sc in best.items():
            agg[e][0] += 1
            agg[e][1] += sc
    if not agg or analyzed == 0:
        return None, 0.0
    # Couverture minimale : 1/3 des valeurs, plancher à 2 (matchRatio du backend).
    floor = max(2, analyzed // 3)
    eligible = {e: c for e, c in agg.items() if c[0] >= floor}
    if not eligible:
        return None, 0.0
    top = sorted(eligible.items(), key=lambda kv: (-kv[1][0], -kv[1][1] / kv[1][0], kv[0]))[0]
    return ENTITY_TO_CATEGORY[top[0]], top[1][1] / top[1][0]


def evaluate(port, lang, label):
    rows, tp, fn, fp, wrong = [], 0, 0, 0, 0
    for table, cols in TRUTH.items():
        for column, expected in cols.items():
            if (table, column) in RESOLVED_UPSTREAM:
                continue
            values = fetch_column(table, column)
            got, score = dominant_category(port, values, lang)
            if expected is None and got is None:
                verdict = "ok (rien)"
            elif expected is None:
                verdict, fp = "FAUX POSITIF", fp + 1
            elif got is None:
                verdict, fn = "MANQUE", fn + 1
            elif got == expected:
                verdict, tp = "ok", tp + 1
            else:
                verdict, wrong = "MAUVAISE CLASSE", wrong + 1
            rows.append((table, column, expected, got, score, verdict))

    attendus = sum(
        1 for t, c in TRUTH.items() for col, e in c.items()
        if e is not None and (t, col) not in RESOLVED_UPSTREAM)
    detectes = tp + wrong + fp
    rappel = tp / attendus if attendus else 0
    precision = tp / detectes if detectes else 0
    f1 = 2 * precision * rappel / (precision + rappel) if precision + rappel else 0
    return {"label": label, "rows": rows, "tp": tp, "fn": fn, "fp": fp,
            "wrong": wrong, "attendus": attendus, "rappel": rappel,
            "precision": precision, "f1": f1}


def main():
    # Les trois configurations réellement distinctes. Le catalogue français
    # déclare ses reconnaisseurs pour « en » comme pour « fr » : la langue de la
    # requête ne change donc que le modèle NER (spaCy) et les reconnaisseurs
    # anglais à motifs, pas les reconnaisseurs FR_*.
    configs = [
        (5004, "en", "image officielle (en)"),
        (5002, "en", "catalogue FR, requête en"),
        (5003, "fr", "catalogue FR, requête fr"),
    ]
    results = []
    for port, lang, label in configs:
        print(f"évaluation : {label} …", file=sys.stderr)
        results.append(evaluate(port, lang, label))

    print()
    print("%-14s %-28s %-18s %-18s %s" % ("TABLE", "COLONNE", "ATTENDU", "DÉTECTÉ", "VERDICT"))
    print("-" * 108)
    base = {(r[0], r[1]): r for r in results[0]["rows"]}
    for row in results[-1]["rows"]:
        table, column, expected, got, score, verdict = row
        b = base[(table, column)]
        marker = "" if b[5] == verdict else f"   [{b[5]} -> {verdict}]"
        print("%-14s %-28s %-18s %-18s %s%s" % (
            table, column, expected or "-", got or "-", verdict, marker))

    print()
    print("%-30s %8s %8s %8s %8s %9s %9s %6s" % (
        "CONFIGURATION", "trouvés", "manqués", "fx.pos", "mauv.cl", "rappel", "précis.", "F1"))
    print("-" * 96)
    for r in results:
        print("%-30s %8d %8d %8d %8d %8.0f%% %8.0f%% %6.2f" % (
            r["label"], r["tp"], r["fn"], r["fp"], r["wrong"],
            r["rappel"] * 100, r["precision"] * 100, r["f1"]))
    print(f"\n{results[0]['attendus']} colonnes personnelles dans le périmètre de "
          f"Presidio ; {len(RESOLVED_UPSTREAM)} autres sont résolues en amont "
          f"(clés de contrôle, inférence de format) et exclues de la mesure.")


if __name__ == "__main__":
    main()
