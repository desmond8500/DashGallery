#!/usr/bin/env python3
"""
DashGallery - serveur local.

Sert la galerie (index.html) et expose une petite API qui télécharge des vidéos
YouTube et Pinterest avec yt-dlp. La galerie récupère ensuite le fichier et
l'écrit directement dans le dossier ouvert.

    pip install -r requirements.txt
    python server.py            # puis ouvrir http://localhost:8000

Options : --port 8000  --host 127.0.0.1
ffmpeg est recommandé (fusion vidéo + audio pour la meilleure qualité).
"""
import argparse
import json
import mimetypes
import os
import re
import shutil
import tempfile
import threading
import time
import uuid
from http import HTTPStatus
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import quote, urlparse

try:
    import yt_dlp
except ImportError:  # le serveur démarre quand même, l'API signale l'absence
    yt_dlp = None

ROOT = Path(__file__).resolve().parent
WORK = Path(tempfile.gettempdir()) / "dashgallery-downloads"
JOB_TTL = 3600  # secondes avant nettoyage d'un téléchargement terminé

ALLOWED_HOSTS = (
    "youtube.com", "youtu.be", "youtube-nocookie.com",
    "pinterest.com", "pin.it",
)
PINTEREST_TLD = re.compile(r"(^|\.)pinterest\.[a-z.]{2,6}$")

QUALITIES = {"best": None, "1080": 1080, "720": 720, "480": 480}

jobs = {}
jobs_lock = threading.Lock()


def platform_of(url):
    try:
        u = urlparse(url)
    except ValueError:
        return None
    if u.scheme not in ("http", "https") or not u.hostname:
        return None
    host = u.hostname.lower()
    for h in ALLOWED_HOSTS:
        if host == h or host.endswith("." + h):
            return "pinterest" if h in ("pinterest.com", "pin.it") else "youtube"
    if PINTEREST_TLD.search(host):  # pinterest.fr, pinterest.co.uk...
        return "pinterest"
    return None


def format_for(quality):
    h = QUALITIES.get(quality)
    lim = f"[height<={h}]" if h else ""
    if shutil.which("ffmpeg"):
        # meilleure vidéo + meilleur audio, fusionnés en mp4
        return (f"bv*{lim}[ext=mp4]+ba[ext=m4a]/bv*{lim}+ba/b{lim}[ext=mp4]/b{lim}/b")
    # sans ffmpeg : uniquement des fichiers déjà complets
    return f"b{lim}[ext=mp4]/b{lim}/b"


def safe_name(name):
    name = re.sub(r'[\\/:*?"<>|\x00-\x1f]', "_", name).strip().lstrip(".")
    return (name or "video")[:180]


def run_job(job):
    job_dir = WORK / job["id"]
    job_dir.mkdir(parents=True, exist_ok=True)

    def hook(d):
        if d["status"] == "downloading":
            total = d.get("total_bytes") or d.get("total_bytes_estimate") or 0
            done = d.get("downloaded_bytes") or 0
            with jobs_lock:
                job["status"] = "downloading"
                job["progress"] = round(done * 100 / total, 1) if total else None
                job["speed"] = d.get("speed")
                job["eta"] = d.get("eta")
        elif d["status"] == "finished":
            with jobs_lock:
                job["status"] = "processing"
                job["progress"] = 100

    opts = {
        "format": format_for(job["quality"]),
        "merge_output_format": "mp4",
        "outtmpl": str(job_dir / "%(title).150B [%(id)s].%(ext)s"),
        "noplaylist": True,
        "quiet": True,
        "no_warnings": True,
        "progress_hooks": [hook],
        "restrictfilenames": False,
    }
    try:
        with yt_dlp.YoutubeDL(opts) as ydl:
            info = ydl.extract_info(job["url"], download=True)
            title = info.get("title") or ""
        files = [p for p in job_dir.iterdir() if p.is_file() and not p.name.endswith((".part", ".ytdl"))]
        if not files:
            raise RuntimeError("aucun fichier produit (le lien contient-il une vidéo ?)")
        f = max(files, key=lambda p: p.stat().st_size)
        with jobs_lock:
            job.update(status="done", progress=100, file=str(f), filename=safe_name(f.name),
                       size=f.stat().st_size, title=title, finished=time.time())
    except Exception as e:  # yt-dlp lève des DownloadError très variés
        msg = re.sub(r"\x1b\[[0-9;]*m", "", str(e)).replace("ERROR: ", "")
        with jobs_lock:
            job.update(status="error", error=msg[:500], finished=time.time())
        shutil.rmtree(job_dir, ignore_errors=True)


def cleanup_loop():
    while True:
        time.sleep(300)
        now = time.time()
        with jobs_lock:
            old = [j for j in jobs.values() if j.get("finished") and now - j["finished"] > JOB_TTL]
            for j in old:
                jobs.pop(j["id"], None)
        for j in old:
            shutil.rmtree(WORK / j["id"], ignore_errors=True)


def public(job):
    keys = ("id", "url", "platform", "quality", "status", "progress", "speed", "eta",
            "filename", "size", "title", "error")
    return {k: job.get(k) for k in keys}


class Handler(SimpleHTTPRequestHandler):
    def __init__(self, *a, **kw):
        super().__init__(*a, directory=str(ROOT), **kw)

    def log_message(self, fmt, *args):
        if not self.path.startswith("/api/jobs/") or "/file" in self.path:
            super().log_message(fmt, *args)

    def send_json(self, data, code=200):
        body = json.dumps(data).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def same_origin(self):
        # refuse les requêtes envoyées par d'autres sites ouverts dans le navigateur
        origin = self.headers.get("Origin")
        return not origin or urlparse(origin).netloc == self.headers.get("Host")

    # ----- GET -----
    def do_GET(self):
        path = urlparse(self.path).path
        if path == "/api/status":
            return self.send_json({"ok": True, "ytdlp": yt_dlp is not None,
                                   "version": getattr(getattr(yt_dlp, "version", None), "__version__", None),
                                   "ffmpeg": bool(shutil.which("ffmpeg"))})
        m = re.fullmatch(r"/api/jobs/([0-9a-f]{32})(/file)?", path)
        if m:
            with jobs_lock:
                job = jobs.get(m.group(1))
                snapshot = dict(job) if job else None
            if not snapshot:
                return self.send_json({"error": "téléchargement inconnu"}, 404)
            if not m.group(2):
                return self.send_json(public(snapshot))
            return self.send_file(snapshot)
        if path.startswith("/api/"):
            return self.send_json({"error": "introuvable"}, 404)
        if path in ("/server.py", "/requirements.txt") or "/." in path:
            return self.send_error(HTTPStatus.NOT_FOUND)
        return super().do_GET()

    def send_file(self, job):
        if job.get("status") != "done" or not os.path.exists(job.get("file", "")):
            return self.send_json({"error": "fichier indisponible"}, 409)
        size = os.path.getsize(job["file"])
        ctype = mimetypes.guess_type(job["filename"])[0] or "application/octet-stream"
        self.send_response(200)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(size))
        self.send_header("Content-Disposition",
                         "attachment; filename*=UTF-8''" + quote(job["filename"]))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        with open(job["file"], "rb") as f:
            shutil.copyfileobj(f, self.wfile, 1024 * 1024)

    # ----- POST / DELETE -----
    def do_POST(self):
        if not self.same_origin():
            return self.send_json({"error": "origine refusée"}, 403)
        if urlparse(self.path).path != "/api/download":
            return self.send_json({"error": "introuvable"}, 404)
        if yt_dlp is None:
            return self.send_json({"error": "yt-dlp n'est pas installé : pip install -r requirements.txt"}, 503)
        try:
            n = int(self.headers.get("Content-Length") or 0)
            data = json.loads(self.rfile.read(min(n, 10000)) or b"{}")
        except (ValueError, json.JSONDecodeError):
            return self.send_json({"error": "requête invalide"}, 400)
        url = str(data.get("url", "")).strip()
        quality = str(data.get("quality", "best"))
        platform = platform_of(url)
        if not platform:
            return self.send_json({"error": "Lien non pris en charge (YouTube ou Pinterest uniquement)."}, 400)
        if quality not in QUALITIES:
            quality = "best"
        job = {"id": uuid.uuid4().hex, "url": url, "platform": platform, "quality": quality,
               "status": "queued", "progress": 0}
        with jobs_lock:
            jobs[job["id"]] = job
        threading.Thread(target=run_job, args=(job,), daemon=True).start()
        self.send_json(public(job), 202)

    def do_DELETE(self):
        if not self.same_origin():
            return self.send_json({"error": "origine refusée"}, 403)
        m = re.fullmatch(r"/api/jobs/([0-9a-f]{32})", urlparse(self.path).path)
        if not m:
            return self.send_json({"error": "introuvable"}, 404)
        with jobs_lock:
            job = jobs.get(m.group(1))
            if job and job["status"] in ("done", "error"):
                jobs.pop(job["id"], None)
            else:
                job = None
        if job:
            shutil.rmtree(WORK / job["id"], ignore_errors=True)
        self.send_json({"ok": bool(job)})


def main():
    ap = argparse.ArgumentParser(description="Serveur local DashGallery")
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=8000)
    args = ap.parse_args()

    WORK.mkdir(parents=True, exist_ok=True)
    threading.Thread(target=cleanup_loop, daemon=True).start()
    srv = ThreadingHTTPServer((args.host, args.port), Handler)
    print(f"DashGallery : http://localhost:{args.port}")
    if yt_dlp is None:
        print("⚠ yt-dlp absent : pip install -r requirements.txt (le téléchargeur sera désactivé)")
    if not shutil.which("ffmpeg"):
        print("⚠ ffmpeg absent : qualité limitée aux fichiers déjà fusionnés (souvent 720p max sur YouTube)")
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
