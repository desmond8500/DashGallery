# Changelog

Format : [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/), versions [SemVer](https://semver.org/lang/fr/).

## [1.0.0] - 2026-11-19

### Ajouté
- Application Windows en un seul fichier (`DashGallery-v1.0.0-windows-x64.exe`), écrite en Go :
  double-clic, s'ouvre dans sa propre fenêtre, aucun serveur à lancer, aucune console.
- Téléchargeur de vidéos YouTube et Pinterest (plusieurs liens, qualité, dossier de destination),
  avec titre en description et tag `#youtube` / `#pinterest` ; yt-dlp installé automatiquement.
- Fonctionnement hors ligne (Vue intégré, service worker), indicateur « Hors ligne ».
- Versions portables : `DashGallery.html` (galerie seule) et `dashgallery.py` (script unique).
- Affichage de la version et avertissement quand une nouvelle release est disponible.

### Existant (galerie v2)
- Index `index.json`, descriptions, catégories, tags, filtres, recherche, tri.
- Renommage, déplacement, suppression, sélection multiple et actions groupées.
