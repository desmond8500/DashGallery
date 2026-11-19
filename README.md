# DashGallery

Galerie locale pour un dossier d'images et de vidéos : index `index.json`, descriptions,
catégories, tags, renommage / déplacement / suppression, sélection multiple, et
**téléchargeur de vidéos YouTube et Pinterest**.

## Lancer

```bash
pip install -r requirements.txt   # yt-dlp
python server.py                  # puis ouvrir http://localhost:8000
```

- **Chrome / Edge** recommandés (accès en écriture au dossier via File System Access API).
- **ffmpeg** recommandé : sans lui, YouTube est limité aux formats déjà fusionnés (souvent 720p).
- Le serveur n'écoute que sur `127.0.0.1` par défaut (`--host` / `--port` pour changer).

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
