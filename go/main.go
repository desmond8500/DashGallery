// DashGallery - version Go : un seul exécutable, sans serveur à lancer.
//
// Double-clic sur DashGallery.exe : l'appli s'ouvre dans sa propre fenêtre
// (Edge ou Chrome en mode application). Le petit serveur interne tourne en
// arrière-plan et s'arrête quand la fenêtre est fermée.
//
// Le téléchargeur utilise yt-dlp, installé automatiquement au premier usage
// (ou posé à côté de l'exe). ffmpeg est utilisé s'il est à côté de l'exe ou
// dans le PATH.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Remplacé à la compilation : -ldflags "-X main.version=v1.2.3"
var version = "dev"

const (
	repo        = "desmond8500/DashGallery"
	basePort    = 17860 // port fixe : même origine d'une session à l'autre (cache, permissions)
	jobTTL      = time.Hour
	idleTimeout = 4 * time.Minute // sans signe de vie de la fenêtre, l'appli se ferme
)

//go:embed all:web
var webFS embed.FS

var (
	appDir   string // %LOCALAPPDATA%\DashGallery
	exeDir   string
	workDir  string
	lastPing atomic.Int64
	jobs     = map[string]*Job{}
	jobsMu   sync.Mutex
	ytdlpMu  sync.Mutex
)

/* ---------- Téléchargements ---------- */

type Job struct {
	ID       string   `json:"id"`
	URL      string   `json:"url"`
	Platform string   `json:"platform"`
	Quality  string   `json:"quality"`
	Status   string   `json:"status"`
	Progress *float64 `json:"progress"`
	Speed    *float64 `json:"speed"`
	ETA      *int     `json:"eta"`
	Filename string   `json:"filename,omitempty"`
	Size     int64    `json:"size,omitempty"`
	Title    string   `json:"title,omitempty"`
	Error    string   `json:"error,omitempty"`

	file     string
	finished time.Time
}

var (
	pinterestTLD = regexp.MustCompile(`(^|\.)pinterest\.[a-z.]{2,6}$`)
	badChars     = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)
	ansi         = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	qualities    = map[string]int{"best": 0, "1080": 1080, "720": 720, "480": 480}
)

func platformOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	is := func(d string) bool { return h == d || strings.HasSuffix(h, "."+d) }
	switch {
	case is("youtube.com"), is("youtu.be"), is("youtube-nocookie.com"):
		return "youtube"
	case is("pin.it"), pinterestTLD.MatchString(h):
		return "pinterest"
	}
	return ""
}

func exeName(n string) string {
	if runtime.GOOS == "windows" {
		return n + ".exe"
	}
	return n
}

func findTool(name string) string {
	for _, d := range []string{exeDir, appDir} {
		p := filepath.Join(d, exeName(name))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

func ytdlpAsset() string {
	switch runtime.GOOS {
	case "windows":
		return "yt-dlp.exe"
	case "darwin":
		return "yt-dlp_macos"
	}
	return "yt-dlp"
}

// Trouve yt-dlp, ou le télécharge (dernière version officielle) dans le dossier de l'appli.
func ensureYtdlp() (string, error) {
	ytdlpMu.Lock()
	defer ytdlpMu.Unlock()
	if p := findTool("yt-dlp"); p != "" {
		return p, nil
	}
	dest := filepath.Join(appDir, exeName("yt-dlp"))
	log.Println("installation de yt-dlp dans", dest)
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Get(
		"https://github.com/yt-dlp/yt-dlp/releases/latest/download/" + ytdlpAsset())
	if err != nil {
		return "", fmt.Errorf("installation de yt-dlp impossible (connexion ?) : %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("installation de yt-dlp impossible : HTTP %d", resp.StatusCode)
	}
	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	if _, err = io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	f.Close()
	return dest, os.Rename(tmp, dest)
}

// Met yt-dlp à jour en arrière-plan (seulement la copie installée par l'appli)
func updateYtdlp() {
	p := filepath.Join(appDir, exeName("yt-dlp"))
	if _, err := os.Stat(p); err != nil {
		return
	}
	c := exec.Command(p, "-U")
	hideWindow(c)
	if out, err := c.CombinedOutput(); err != nil {
		log.Println("mise à jour yt-dlp :", err, strings.TrimSpace(string(out)))
	}
}

func formatFor(q string, ffmpeg bool) string {
	lim := ""
	if h := qualities[q]; h > 0 {
		lim = fmt.Sprintf("[height<=%d]", h)
	}
	if ffmpeg {
		return fmt.Sprintf("bv*%[1]s[ext=mp4]+ba[ext=m4a]/bv*%[1]s+ba/b%[1]s[ext=mp4]/b%[1]s/b", lim)
	}
	return fmt.Sprintf("b%[1]s[ext=mp4]/b%[1]s/b", lim)
}

func setJob(j *Job, f func(*Job)) {
	jobsMu.Lock()
	f(j)
	jobsMu.Unlock()
}

func fail(j *Job, msg string) {
	setJob(j, func(j *Job) { j.Status, j.Error, j.finished = "error", msg, time.Now() })
	os.RemoveAll(filepath.Join(workDir, j.ID))
}

func numOrNil(s string) *float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func runJob(j *Job) {
	setJob(j, func(j *Job) { j.Status = "installing" })
	ytdlp, err := ensureYtdlp()
	if err != nil {
		fail(j, err.Error())
		return
	}
	dir := filepath.Join(workDir, j.ID)
	os.MkdirAll(dir, 0o755)
	ffmpeg := findTool("ffmpeg")

	args := []string{
		"--newline", "--progress", "--no-playlist", "--no-warnings", "--no-simulate",
		"--encoding", "utf-8",
		"-f", formatFor(j.Quality, ffmpeg != ""), "--merge-output-format", "mp4",
		"-o", filepath.Join(dir, "%(title).150B [%(id)s].%(ext)s"),
		"--progress-template", "download:DGP %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s %(progress.eta)s %(progress.speed)s",
		"--print", "before_dl:DGT %(title)s",
	}
	if ffmpeg != "" {
		args = append(args, "--ffmpeg-location", ffmpeg)
	}
	args = append(args, "--", j.URL)

	cmd := exec.Command(ytdlp, args...)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	hideWindow(cmd)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	setJob(j, func(j *Job) { j.Status = "downloading"; j.Progress = nil })
	if err := cmd.Start(); err != nil {
		fail(j, "lancement de yt-dlp impossible : "+err.Error())
		return
	}

	var errLines []string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			l := strings.TrimSpace(ansi.ReplaceAllString(sc.Text(), ""))
			if strings.HasPrefix(l, "ERROR:") {
				errLines = append(errLines, strings.TrimSpace(strings.TrimPrefix(l, "ERROR:")))
			}
		}
	}()
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "DGT "):
			setJob(j, func(j *Job) { j.Title = strings.TrimSpace(l[4:]) })
		case strings.HasPrefix(l, "DGP "):
			f := strings.Fields(l[4:])
			for len(f) < 5 {
				f = append(f, "NA")
			}
			done, total := numOrNil(f[0]), numOrNil(f[1])
			if total == nil {
				total = numOrNil(f[2])
			}
			setJob(j, func(j *Job) {
				j.Status = "downloading"
				j.Progress = nil
				if done != nil && total != nil && *total > 0 {
					p := float64(int(*done*10000 / *total)) / 100
					j.Progress = &p
					if p >= 100 {
						j.Status = "processing"
					}
				}
				if e := numOrNil(f[3]); e != nil {
					v := int(*e)
					j.ETA = &v
				}
				j.Speed = numOrNil(f[4])
			})
		}
	}
	wg.Wait()
	if err := cmd.Wait(); err != nil {
		msg := strings.Join(errLines, " ; ")
		if msg == "" {
			msg = "yt-dlp a échoué (" + err.Error() + ")"
		}
		if len(msg) > 500 {
			msg = msg[:500]
		}
		fail(j, msg)
		return
	}

	// Le plus gros fichier terminé du dossier = la vidéo
	var best string
	var bestSize int64 = -1
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasSuffix(n, ".part") || strings.HasSuffix(n, ".ytdl") {
			continue
		}
		if info, err := e.Info(); err == nil && info.Size() > bestSize {
			best, bestSize = n, info.Size()
		}
	}
	if best == "" {
		fail(j, "aucun fichier produit (le lien contient-il une vidéo ?)")
		return
	}
	name := strings.TrimLeft(strings.TrimSpace(badChars.ReplaceAllString(best, "_")), ".")
	if name == "" {
		name = "video.mp4"
	}
	if r := []rune(name); len(r) > 180 {
		name = string(r[:180])
	}
	full := 100.0
	setJob(j, func(j *Job) {
		j.Status, j.Progress, j.file, j.Filename, j.Size, j.finished = "done", &full, filepath.Join(dir, best), name, bestSize, time.Now()
	})
}

func cleanupLoop() {
	for range time.Tick(5 * time.Minute) {
		jobsMu.Lock()
		for id, j := range jobs {
			if !j.finished.IsZero() && time.Since(j.finished) > jobTTL {
				delete(jobs, id)
				os.RemoveAll(filepath.Join(workDir, id))
			}
		}
		jobsMu.Unlock()
	}
}

func activeJobs() int {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	n := 0
	for _, j := range jobs {
		if j.finished.IsZero() {
			n++
		}
	}
	return n
}

/* ---------- HTTP ---------- */

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && u.Host == r.Host
}

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

var jobPath = regexp.MustCompile(`^/api/jobs/([0-9a-f]{32})(/file)?$`)

func apiHandler(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if r.Method != http.MethodGet && !sameOrigin(r) {
		writeJSON(w, 403, map[string]string{"error": "origine refusée"})
		return
	}
	switch {
	case p == "/api/status":
		writeJSON(w, 200, map[string]any{
			"ok": true, "app": "dashgallery", "appVersion": version, "ytdlp": true,
			"ytdlpInstalled": findTool("yt-dlp") != "", "ffmpeg": findTool("ffmpeg") != "",
		})
	case p == "/api/ping":
		lastPing.Store(time.Now().Unix())
		w.WriteHeader(204)
	case p == "/api/update":
		writeJSON(w, 200, checkUpdate())
	case p == "/api/download" && r.Method == http.MethodPost:
		var in struct{ URL, Quality string }
		if err := json.NewDecoder(io.LimitReader(r.Body, 10000)).Decode(&in); err != nil {
			writeJSON(w, 400, map[string]string{"error": "requête invalide"})
			return
		}
		in.URL = strings.TrimSpace(in.URL)
		pf := platformOf(in.URL)
		if pf == "" {
			writeJSON(w, 400, map[string]string{"error": "Lien non pris en charge (YouTube ou Pinterest uniquement)."})
			return
		}
		if _, ok := qualities[in.Quality]; !ok {
			in.Quality = "best"
		}
		zero := 0.0
		j := &Job{ID: newID(), URL: in.URL, Platform: pf, Quality: in.Quality, Status: "queued", Progress: &zero}
		jobsMu.Lock()
		jobs[j.ID] = j
		snap := *j
		jobsMu.Unlock()
		go runJob(j)
		writeJSON(w, 202, snap)
	default:
		m := jobPath.FindStringSubmatch(p)
		if m == nil {
			writeJSON(w, 404, map[string]string{"error": "introuvable"})
			return
		}
		jobsMu.Lock()
		j := jobs[m[1]]
		var snap Job
		if j != nil {
			snap = *j
		}
		if j != nil && r.Method == http.MethodDelete && !j.finished.IsZero() {
			delete(jobs, j.ID)
		}
		jobsMu.Unlock()
		switch {
		case j == nil:
			writeJSON(w, 404, map[string]string{"error": "téléchargement inconnu"})
		case r.Method == http.MethodDelete:
			if !snap.finished.IsZero() {
				os.RemoveAll(filepath.Join(workDir, snap.ID))
			}
			writeJSON(w, 200, map[string]bool{"ok": !snap.finished.IsZero()})
		case m[2] == "":
			writeJSON(w, 200, snap)
		default:
			if snap.Status != "done" {
				writeJSON(w, 409, map[string]string{"error": "fichier indisponible"})
				return
			}
			f, err := os.Open(snap.file)
			if err != nil {
				writeJSON(w, 409, map[string]string{"error": "fichier indisponible"})
				return
			}
			defer f.Close()
			ct := mime.TypeByExtension(filepath.Ext(snap.Filename))
			if ct == "" {
				ct = "application/octet-stream"
			}
			w.Header().Set("Content-Type", ct)
			w.Header().Set("Content-Length", strconv.FormatInt(snap.Size, 10))
			w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(snap.Filename))
			w.Header().Set("Cache-Control", "no-store")
			io.Copy(w, f)
		}
	}
}

func staticHandler(sub fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		data, err := fs.ReadFile(sub, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(data)
	}
}

/* ---------- Versions ---------- */

var (
	updMu    sync.Mutex
	updCache map[string]any
	updAt    time.Time
)

func parseVer(v string) []int {
	v = strings.TrimPrefix(strings.SplitN(v, "-", 2)[0], "v")
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

func newer(latest, current string) bool {
	a, b := parseVer(latest), parseVer(current)
	if a == nil || b == nil {
		return false
	}
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// Interroge la dernière release GitHub (au plus une fois par heure)
func checkUpdate() map[string]any {
	updMu.Lock()
	defer updMu.Unlock()
	if updCache != nil && time.Since(updAt) < time.Hour {
		return updCache
	}
	res := map[string]any{"current": version, "newer": false}
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 6 * time.Second}).Do(req)
	if err != nil {
		res["error"] = "hors ligne"
		return res
	}
	defer resp.Body.Close()
	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if resp.StatusCode == 200 && json.NewDecoder(resp.Body).Decode(&rel) == nil {
		res["latest"], res["url"] = rel.TagName, rel.HTMLURL
		res["newer"] = newer(rel.TagName, version)
	}
	updCache, updAt = res, time.Now()
	return res
}

/* ---------- Démarrage ---------- */

// Une instance tourne déjà ? On lui envoie simplement une nouvelle fenêtre.
func existingInstance(port int) bool {
	resp, err := (&http.Client{Timeout: 700 * time.Millisecond}).Get(fmt.Sprintf("http://127.0.0.1:%d/api/status", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var s struct{ App string }
	return json.NewDecoder(resp.Body).Decode(&s) == nil && s.App == "dashgallery"
}

func listen() (net.Listener, string, error) {
	for port := basePort; port < basePort+20; port++ {
		if existingInstance(port) {
			return nil, fmt.Sprintf("http://localhost:%d/", port), nil
		}
		if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			return ln, fmt.Sprintf("http://localhost:%d/", port), nil
		}
	}
	return nil, "", errors.New("aucun port libre")
}

func main() {
	exe, _ := os.Executable()
	exeDir = filepath.Dir(exe)
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	appDir = filepath.Join(base, "DashGallery")
	workDir = filepath.Join(appDir, "tmp")
	os.MkdirAll(workDir, 0o755)
	if lf, err := os.OpenFile(filepath.Join(appDir, "dashgallery.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		log.SetOutput(io.MultiWriter(lf, os.Stderr))
	}
	log.Println("DashGallery", version)
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
	mime.AddExtensionType(".svg", "image/svg+xml")
	mime.AddExtensionType(".mp4", "video/mp4")
	mime.AddExtensionType(".webm", "video/webm")

	noWindow := len(os.Args) > 1 && os.Args[1] == "--no-window"
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("DashGallery", version)
		return
	}

	ln, appURL, err := listen()
	if err != nil {
		fatal(err.Error())
	}
	if ln == nil { // déjà lancé
		openWindow(appURL)
		return
	}

	sub, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", apiHandler)
	mux.HandleFunc("/", staticHandler(sub))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	go cleanupLoop()
	go updateYtdlp()
	log.Println("écoute sur", appURL)

	quit := make(chan struct{})
	var once sync.Once
	stop := func(why string) { once.Do(func() { log.Println("arrêt :", why); close(quit) }) }

	if !noWindow {
		go func() {
			start := time.Now()
			waited := openWindow(appURL)
			// Fenêtre dédiée fermée (et pas juste confiée à un navigateur déjà ouvert)
			if waited && time.Since(start) > 5*time.Second {
				for activeJobs() > 0 {
					time.Sleep(2 * time.Second)
				}
				stop("fenêtre fermée")
			}
		}()
		// Filet de sécurité : plus aucune page ouverte depuis un moment
		lastPing.Store(time.Now().Unix())
		go func() {
			for range time.Tick(15 * time.Second) {
				if activeJobs() == 0 && time.Since(time.Unix(lastPing.Load(), 0)) > idleTimeout {
					stop("plus de fenêtre ouverte")
					return
				}
			}
		}()
	} else {
		fmt.Println("DashGallery", version, ":", appURL, "(Ctrl+C pour arrêter)")
	}

	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// Cherche Edge ou Chrome pour ouvrir l'appli dans une fenêtre sans barre d'adresse.
func browserCandidates() []string {
	var c []string
	switch runtime.GOOS {
	case "windows":
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"} {
			b := os.Getenv(env)
			if b == "" {
				continue
			}
			c = append(c, filepath.Join(b, `Microsoft\Edge\Application\msedge.exe`),
				filepath.Join(b, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(b, `BraveSoftware\Brave-Browser\Application\brave.exe`))
		}
	case "darwin":
		c = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"}
	default:
		for _, n := range []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
			if p, err := exec.LookPath(n); err == nil {
				c = append(c, p)
			}
		}
	}
	return c
}

// Ouvre la fenêtre de l'appli ; renvoie true si on a pu attendre sa fermeture.
func openWindow(u string) bool {
	for _, b := range browserCandidates() {
		if _, err := os.Stat(b); err != nil {
			continue
		}
		cmd := exec.Command(b, "--app="+u,
			"--user-data-dir="+filepath.Join(appDir, "profil"),
			"--no-first-run", "--no-default-browser-check", "--window-size=1400,900")
		if cmd.Start() == nil {
			cmd.Wait()
			return true
		}
	}
	// Repli : navigateur par défaut (l'appli s'arrête via le délai d'inactivité)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if err := cmd.Start(); err != nil {
		log.Println("ouverture du navigateur impossible :", err, "- ouvrez", u)
	}
	return false
}
