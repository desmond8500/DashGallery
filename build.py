#!/usr/bin/env python3
"""
Construit les versions portables (un seul fichier chacune) dans dist/ :

  dist/DashGallery.html  galerie seule, Vue intégré : double-clic, marche hors ligne
                         (pas de téléchargeur : il a besoin du serveur)
  dist/dashgallery.py    serveur + galerie + Vue dans un seul script Python
                         (python dashgallery.py ; yt-dlp requis pour le téléchargeur)

    python build.py

Vue est pris dans vendor/vue.global.prod.js, ou téléchargé s'il manque (connexion requise
la première fois). L'exécutable Windows est produit à partir de dist/dashgallery.py par
PyInstaller (voir .github/workflows/build.yml).
"""
import base64
import re
import sys
from pathlib import Path
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parent
DIST = ROOT / "dist"
VUE_URL = "https://unpkg.com/vue@3.5.13/dist/vue.global.prod.js"
VUE_FILE = ROOT / "vendor" / "vue.global.prod.js"

VUE_TAGS = re.compile(
    r'<script src="vendor/vue\.global\.prod\.js"></script>\s*<script>window\.Vue \|\|[^\n]*</script>'
)


def get_vue():
    if not VUE_FILE.exists():
        print("Téléchargement de Vue...")
        with urlopen(VUE_URL, timeout=30) as r:
            data = r.read()
        VUE_FILE.parent.mkdir(exist_ok=True)
        VUE_FILE.write_bytes(data)
    vue = VUE_FILE.read_text(encoding="utf-8")
    if "createApp" not in vue:
        sys.exit("vendor/vue.global.prod.js ne ressemble pas à Vue 3")
    return vue


def inline_vue(html, vue):
    if not VUE_TAGS.search(html):
        sys.exit("Balises <script> de Vue introuvables dans index.html")
    safe = vue.replace("</script", "<\\/script")
    return VUE_TAGS.sub(lambda _: "<script>" + safe + "</script>", html, count=1)


def main():
    DIST.mkdir(exist_ok=True)
    vue = get_vue()
    html = inline_vue((ROOT / "index.html").read_text(encoding="utf-8"), vue)
    icon = (ROOT / "icon.svg").read_bytes()
    icon_uri = "data:image/svg+xml;base64," + base64.b64encode(icon).decode()

    # 1) HTML autonome : ni manifeste ni service worker (inutiles en file://), icône intégrée
    standalone = html.replace('<link rel="manifest" href="manifest.webmanifest">\n', "")
    standalone = standalone.replace('href="icon.svg"', f'href="{icon_uri}"')
    (DIST / "DashGallery.html").write_text(standalone, encoding="utf-8")

    # 2) Script Python autonome : fichiers intégrés dans le dictionnaire EMBEDDED
    assets = {
        "index.html": html.encode("utf-8"),
        "sw.js": (ROOT / "sw.js").read_bytes(),
        "manifest.webmanifest": (ROOT / "manifest.webmanifest").read_bytes(),
        "icon.svg": icon,
        "vendor/vue.global.prod.js": vue.encode("utf-8"),
    }
    lines = ["EMBEDDED = {"]
    for name, data in assets.items():
        lines.append(f"    {name!r}: {base64.b64encode(data).decode()!r},")
    lines.append("}")
    server = (ROOT / "server.py").read_text(encoding="utf-8")
    marker = "EMBEDDED = {}  # @@EMBEDDED@@"
    if server.count(marker) != 1:
        sys.exit("Repère EMBEDDED introuvable dans server.py")
    (DIST / "dashgallery.py").write_text(server.replace(marker, "\n".join(lines)), encoding="utf-8")

    for f in ("DashGallery.html", "dashgallery.py"):
        print(f"✓ dist/{f}  ({(DIST / f).stat().st_size / 1024:.0f} Ko)")


if __name__ == "__main__":
    main()
