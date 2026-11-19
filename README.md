# DashGallery

Galerie locale pour un dossier d'images et de vidéos : index `index.json`, descriptions,
catégories, tags, renommage / déplacement / suppression, sélection multiple, et
**téléchargeur de vidéos YouTube et Pinterest**.

## Windows : un seul fichier

Téléchargez **`DashGallery-vX.Y.Z-windows-x64.exe`** depuis la page
[Releases](https://github.com/desmond8500/DashGallery/releases) et double-cliquez : l'appli
s'ouvre dans sa propre fenêtre (Edge ou Chrome en mode application). Pas d'installation, pas de
Python, pas de serveur à lancer, pas de console. Fermer la fenêtre arrête l'appli.

- **Hors ligne** : la galerie fonctionne sans connexion (tout est dans l'exe).
- **Téléchargeur** : yt-dlp est téléchargé automatiquement au premier usage dans
  `%LOCALAPPDATA%\DashGallery` puis mis à jour à chaque lancement. Vous pouvez aussi poser
  `yt-dlp.exe` à côté de l'exe.
- **ffmpeg** (meilleure qualité YouTube) : posez `ffmpeg.exe` à côté de l'exe ou dans le PATH.
- La version s'affiche dans l'en-tête ; un lien apparaît quand une nouvelle release existe.
- Journal : `%LOCALAPPDATA%\DashGallery\dashgallery.log`. `DashGallery.exe --version` affiche la version.

Windows SmartScreen peut avertir la première fois (exe non signé) : « Informations
complémentaires » → « Exécuter quand même ».

Autres fichiers de chaque release : version ARM64, Linux, `DashGallery-vX.Y.Z.html` (galerie seule,
double-clic, sans téléchargeur), `dashgallery-vX.Y.Z.py` (script Python unique), `SHA256SUMS.txt`.

## Versions et releases

Numérotation [SemVer](https://semver.org/lang/fr/) (`v1.0.0`, `v1.1.0` pour une nouveauté, `v1.0.1`
pour une correction, `v2.0.0-beta.1` pour une préversion), historique dans [CHANGELOG.md](CHANGELOG.md).

Publier une version :

```bash
# 1. ajouter la section de la version dans CHANGELOG.md, committer sur main
git tag v1.1.0
git push origin v1.1.0
```

GitHub Actions construit alors tous les fichiers et crée la release avec les notes générées.
Chaque push et chaque pull request produit aussi des builds de test (onglet **Actions** → *Artifacts*).

## Construire soi-même

```bash
python build.py --version v1.0.0      # dist/ + go/web/ (télécharge Vue la première fois)
cd go
go build -ldflags "-H=windowsgui -X main.version=v1.0.0" -o DashGallery.exe .   # depuis Windows
GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui -X main.version=v1.0.0" -o DashGallery.exe .  # ailleurs
```

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
