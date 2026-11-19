# DashGallery

Galerie locale pour un dossier d'images et de vidéos : index `index.json`, descriptions,
catégories, tags, renommage / déplacement / suppression, sélection multiple, et
**téléchargeur de vidéos YouTube et Pinterest**.

## Version portable (un seul fichier)

Les fichiers prêts à l'emploi sont générés automatiquement par GitHub Actions
(onglet **Actions** → dernier build → *Artifacts*, ou page **Releases** pour un tag `v*`) :

| Fichier | Usage | Téléchargeur |
|---|---|---|
| `DashGallery.exe` (Windows) / `DashGallery` (Linux) | double-clic : lance le serveur et ouvre le navigateur. Python inutile. | ✓ (yt-dlp inclus) |
| `dashgallery.py` | `python dashgallery.py` (avec `pip install yt-dlp`) | ✓ |
| `DashGallery.html` | double-clic, aucun serveur | ✗ |

Tout fonctionne **hors ligne** (Vue est intégré), sauf bien sûr le téléchargement de vidéos.
Pour la meilleure qualité YouTube, posez `ffmpeg.exe` (ou `ffmpeg`) à côté de l'exécutable.

Pour les construire soi-même : `python build.py` → dossier `dist/`
(puis `pyinstaller --onefile --name DashGallery dist/dashgallery.py` pour l'exécutable).

## Lancer depuis les sources

```bash
pip install -r requirements.txt   # yt-dlp
python server.py                  # ouvre http://localhost:8000
```

Au premier lancement connecté, `server.py` enregistre Vue dans `vendor/` : ensuite la
galerie marche **hors ligne**. Un service worker la garde aussi en cache, donc
`http://localhost:8000` s'ouvre même sans connexion.

- **Chrome / Edge** recommandés (accès en écriture au dossier via File System Access API).
- **ffmpeg** recommandé : sans lui, YouTube est limité aux formats déjà fusionnés (souvent 720p).
- Le serveur n'écoute que sur `127.0.0.1` par défaut (`--host`, `--port`, `--no-browser`).
  Si le port est pris, il essaie le suivant.

Sans le serveur, `index.html` peut toujours être ouvert directement : la galerie marche,
seul le téléchargeur est indisponible.

## Téléchargeur

Bouton **⬇ Télécharger une vidéo** → collez un ou plusieurs liens (YouTube, `youtu.be`,
Pinterest, `pin.it`), choisissez la qualité et le dossier de destination.

1. `server.py` télécharge la vidéo avec yt-dlp dans un dossier temporaire.
2. La galerie récupère le fichier et l'écrit dans le dossier ouvert (par défaut `téléchargements/`),
   avec le titre en description et le tag `#youtube` ou `#pinterest`, puis met `index.json` à jour.
3. Sans dossier ouvert en écriture, le fichier part dans les Téléchargements du navigateur.

Si un site change et que les téléchargements échouent : `pip install -U yt-dlp`.

> Ne téléchargez que des contenus que vous avez le droit de conserver (vos propres vidéos,
> licences libres, autorisation de l'auteur). Les conditions d'utilisation de YouTube et
> Pinterest encadrent le téléchargement.

## API du serveur

| Méthode | Route | Rôle |
|---|---|---|
| GET | `/api/status` | yt-dlp / ffmpeg disponibles ? |
| POST | `/api/download` `{url, quality}` | lance un téléchargement (`best`, `1080`, `720`, `480`) |
| GET | `/api/jobs/<id>` | progression |
| GET | `/api/jobs/<id>/file` | fichier terminé |
| DELETE | `/api/jobs/<id>` | supprime le fichier temporaire |
