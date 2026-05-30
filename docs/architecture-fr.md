# Beeket — Architecture

> 🇬🇧 English version: [architecture-en.md](./architecture-en.md)

---

## 1. Vue d'ensemble

Beeket est un serveur LLM local compatible [Ollama](https://ollama.com), écrit en Go.
Il expose une API REST qui permet aux clients de télécharger, gérer et exécuter des
modèles au format GGUF, en utilisant [llama.cpp](https://github.com/ggml-org/llama.cpp)
comme moteur d'inférence. La bibliothèque C++ llama.cpp est accessible via
[Yzma](https://github.com/hybridgroup/yzma) (`hybridgroup/yzma`), une enveloppe Go pure sans CGo (utilise `purego` et `jupiterrider/ffi` pour le chargement dynamique)
qui expose l'API C de llama.cpp sans embarquer la bibliothèque partagée dans le binaire.

Le dépôt produit un **binaire unique** (`beeket`) qui joue à la fois le rôle de serveur
(`beeket serve`) et d'interface CLI de gestion (`beeket pull`, `list`, `show`, `rm`,
`run`, `ps`). Le binaire est volontairement réduit : la bibliothèque partagée llama.cpp
(`.so`/`.dylib`/`.dll`) est chargée à l'exécution depuis un répertoire fourni par
l'utilisateur ou téléchargée automatiquement via `--auto-install-lib`.

La structure des packages internes reflète trois séparations nettes des responsabilités :

- **Un seul point FFI** — seul `internal/engine` communique avec Yzma/llama.cpp.
- **Un seul gestionnaire de concurrence** — seul `internal/scheduler` gère les
  goroutines et le chargement des modèles.
- **Un seul gestionnaire de persistance** — seul `internal/store` connaît la disposition
  des fichiers sur disque ; `internal/models` fournit le registre logique par-dessus.

---

## 2. Flux de traitement des requêtes

```
Client HTTP
    │
    ▼
┌─────────────────────────────────────────────────┐
│  api.Server  (internal/api/server.go)           │
│  - Mux HTTP (Go 1.22, routage method+path)      │
│  - metrics.Middleware (compteurs Prometheus)    │
│  - journalisation des requêtes (debug)          │
└──────────────────────┬──────────────────────────┘
                       │
            ┌──────────▼──────────┐
            │   api.Handler       │  internal/api/handlers.go
            │                     │
            │  - injectNoThink    │  ← injection /no_think (modèles pensants)
            │  - resolveFormat    │  ← sélection grammaire JSON / JSON Schema
            │  - réécriture outils│  ← injection prompt système, réécriture rôles
            │  - buildChatPrompt  │  ← template ChatML / template natif
            └──────┬──────────────┘
                   │
       ┌───────────▼────────────┐    ┌──────────────────────┐
       │  scheduler.Scheduler   │◄──►│  models.Manager       │
       │  (internal/scheduler)  │    │  (internal/models)    │
       │                        │    │                        │
       │  - Pool de Workers     │    │  - Résoudre name:tag  │
       │  - File d'attente (32) │    │  - Lire/écrire manifest│
       │  - Éviction LRU        │    │  - Alias table        │
       │  - TTL keep-alive      │    └───────────────────────┘
       │  - Pool EmbedWorkers   │
       └──────────┬─────────────┘
                  │
       ┌──────────▼─────────────┐    ┌──────────────────────┐
       │  engine.Session        │    │  engine.EmbedSession  │
       │  (internal/engine)     │    │  (internal/engine)    │
       │                        │    │                        │
       │  - llama.cpp via Yzma  │    │  - Vecteurs L2-normés │
       │  - Chaîne de samplers  │    │  - GetEmbeddingsSeq   │
       │  - Samplers grammaire  │    └──────────────────────-┘
       │  - Template de chat    │
       └──────────┬─────────────┘
                  │
       ┌──────────▼─────────────┐
       │  llama.cpp (.so/.dylib)│
       │  chargé via Yzma FFI   │
       └────────────────────────┘

Préoccupations transversales :
  internal/metrics     — Middleware Prometheus + collecteurs sur chaque requête
  internal/config      — flags → variables d'env → config TOML, appliqués au démarrage
  internal/libinstall  — téléchargement optionnel de la bibliothèque partagée llama.cpp
  internal/tools       — injection prompt système, parsing JSON d'appels d'outils (BuildGrammar disponible mais non utilisé dans le chemin actuel)
  internal/jsongrammar — JSON GBNF canonique pour sortie structurée (format: "json")
  internal/download    — téléchargeur HTTPS repris (pull de modèles)
  internal/store       — blobs + manifests sur disque ($XDG_DATA_HOME/beeket)
```

---

## 3. Tableau des responsabilités par package

| Package | Chemin | Types clés | Responsabilité |
|---|---|---|---|
| `cmd/beeket` | `cmd/beeket/main.go` | — | Point d'entrée du binaire unique. Câble `config → libinstall → store → engine → models → scheduler → api`. Fournit `serve` et toutes les sous-commandes client (`pull`, `list`, `show`, `rm`, `run`, `ps`). |
| `internal/api` | `internal/api/` | `Server`, `Handler`, `HandlerConfig` | API HTTP compatible Ollama. Routage, streaming NDJSON, construction du prompt de chat, interception des appels d'outils, sélection de grammaire pour sortie structurée, enregistrement des métriques. |
| `internal/engine` | `internal/engine/engine.go` | `Engine`, `Model`, `Session`, `EmbedSession`, `SamplerOptions`, `GenerateOptions` | Unique point FFI vers Yzma/llama.cpp. Cycle de vie de la bibliothèque, chargement des modèles, sessions d'inférence, construction de la chaîne de samplers, rendu du template de chat, extraction d'embeddings. |
| `internal/scheduler` | `internal/scheduler/scheduler.go` | `Scheduler`, `Worker`, `EmbedWorker`, `Config`, `LoadedInfo` | Couche de concurrence. Un goroutine `Worker` par modèle chargé, avec un canal de requêtes de 32 emplacements. Applique `MaxLoaded`, l'éviction LRU et l'éviction keep-alive. Expose `Generate`, `Embed`, `LoadedModels`. |
| `internal/models` | `internal/models/` | `Manager`, `Manifest`, `Details` | Registre logique des modèles. Résout les références `name[:tag]`, lit/écrit les manifests, gère la table d'alias, inspecte les métadonnées GGUF. |
| `internal/download` | `internal/download/` | `Get`, `Resolve`, `TmpFilename` | Téléchargeur HTTPS repris pour les blobs `.gguf`. Émet des callbacks de progression consommés par `api.Pull`. Résout les URLs raccourcies HuggingFace. |
| `internal/store` | `internal/store/store.go` | `Store` | Store sur disque adressé par contenu. Gère les blobs, les manifests JSON, les suppressions. Seul package qui connaît la disposition sous `$XDG_DATA_HOME/beeket`. |
| `internal/metrics` | `internal/metrics/` | `Middleware`, `Register`, `InferenceRequestsTotal`, `InferenceDuration` | Registre Prometheus : informations de build, uptime, compteurs de requêtes, histogrammes de latence, compteurs de débit en tokens, jauge de modèles chargés. Le middleware HTTP enveloppe chaque réponse. |
| `internal/tools` | `internal/tools/` | `Tool`, `ToolCall`, `RenderToolPreface`, `RewriteToolMessages`, `ParseToolCall` | Construit le préambule système à partir des schémas d'outils ; analyse le JSON d'appel d'outil depuis la sortie du modèle. Réécrit les messages de rôle `tool` pour la compatibilité du template. (`BuildGrammar` existe dans le package mais n'est pas appelé dans le chemin de handler actuel.) |
| `internal/jsongrammar` | `internal/jsongrammar/jsongrammar.go` | `JSONGrammar`, `ValidateSchema` | Source de vérité unique pour la grammaire GBNF JSON canonique utilisée dans les requêtes `format: "json"`. Valide également le JSON de réponse contre un JSON Schema après génération. |
| `internal/config` | `internal/config/config.go` | `Config`, `Load`, `ApplyEnv`, `Validate` | Schéma de configuration, chargeur TOML, surcouche de variables d'env (`BEEKET_*`), surcouche des flags CLI, validation et résolution des chemins XDG. Priorité : défauts → TOML → env → flags. |
| `internal/libinstall` | `internal/libinstall/` | `Ensure`, `Options` | `--auto-install-lib` optionnel : détecte la plateforme/le backend (cpu/cuda/metal/vulkan/rocm) et télécharge la bibliothèque partagée llama.cpp correspondante via Yzma dans `lib-dir`. |
| `internal/version` | `internal/version/version.go` | `Version`, `Commit`, `BuildDate` | Variables de version au moment de la compilation, renseignées via `-ldflags`. |
| `pkg/client` | `pkg/client/` | `Client` | Client Go public pour l'API HTTP. Importable par des tiers ; utilisé en interne par les sous-commandes CLI. |

---

## 4. Déroulement des flux de données

### 4.1 Requête de chat (`POST /api/chat`)

1. **`api.Handler.Chat`** décode le JSON `ChatRequest` depuis le corps HTTP.
2. Si `tools[]` est présent, `tools.RewriteToolMessages` convertit les messages
   de rôle `tool` en rôle `user` (le template de yzma ne connaît pas le rôle `tool`),
   et `tools.RenderToolPreface` préfixe une instruction structurée dans le message système.
3. `resolveFormat(req.Format)` est appelé : retourne la chaîne `jsongrammar.JSONGrammar`
   canonique et, pour les objets JSON Schema, la map de schéma pour validation post-génération.
4. Si `think: false` ou une sortie structurée est demandée, `injectNoThink` ajoute
   `/no_think` au dernier message utilisateur (exigence Qwen3) et, pour le mode JSON,
   préfixe un prompt système JSON-only. Un stop-string `</think>` est également ajouté.
5. `buildChatPrompt` applique ChatML (ou le template natif du modèle via
   `engine.Session.ApplyChatTemplate`) pour produire la chaîne de prompt.
6. **`scheduler.Scheduler.Generate`** recherche (ou charge) le `Worker` pour le
   `name:tag` demandé et enfile une `Request` dans le canal de 32 emplacements du worker.
7. **`Worker.run`** défile la requête et appelle **`engine.Session.Generate`**,
   qui tokenise le prompt, exécute la boucle de décodage et diffuse les tokens au
   callback `out`.
8. Chaque token déclenche le callback `out` dans `Handler.Chat`, qui diffuse soit
   un chunk NDJSON `ChatResponse`, soit l'ajoute à un tampon (non-streaming / outils).
9. Après génération : suppression des stop-strings, validation optionnelle du schéma
   JSON (`jsongrammar.ValidateSchema`), parsing des appels d'outils (`tools.ParseToolCall`)
   et enregistrement des métriques Prometheus.
10. Le `ChatResponse` final (done: true) est écrit dans la réponse HTTP.

### 4.2 Requête d'embeddings (`POST /api/embeddings` ou `/api/embed`)

1. **`api.Handler.Embeddings`** normalise le champ `input` en `[]string`
   (supporte string, `[]string`, et le champ legacy `prompt` pour compatibilité Ollama).
2. Pour chaque chaîne d'entrée, **`scheduler.Scheduler.Embed`** recherche (ou charge)
   l'`EmbedWorker` pour le modèle. Les workers embed sont indexés sous `name:tag#embed`
   dans une map séparée pour ne pas déplacer les workers de génération.
3. **`EmbedWorker.run`** transmet le texte à **`engine.EmbedSession.Embed`**, qui :
   - tokenise le texte avec `llama.Tokenize` ;
   - après l'init du contexte, appelle `llama.SetEmbeddings(ctx, true)` pour activer le mode sortie d'embeddings ;
   - exécute `llama.Decode` sur le batch de tokens ;
   - lit le vecteur d'embedding poolé via `llama.GetEmbeddingsSeq` ;
   - copie le vecteur hors de la mémoire FFI et le normalise en L2.
4. Le handler collecte tous les vecteurs par entrée et les comptes de tokens, enregistre
   les métriques Prometheus et écrit un unique objet JSON `EmbeddingsResponse`.

### 4.3 Appel d'outil (Tool Call)

1. Le client envoie `POST /api/chat` avec un tableau `tools[]`.
2. `Handler.Chat` convertit chaque outil en `tools.Tool` et appelle
   `tools.RenderToolPreface(toolsList)` — ceci produit une description textuelle compacte
   de tous les outils avec leurs paramètres.
3. Le préambule est préfixé dans le message système. `/no_think` est ajouté
   au dernier message utilisateur pour supprimer le raisonnement chain-of-thought.
4. Le prompt est construit avec le template ChatML existant (pas le template natif du moteur,
   car les définitions d'outils sont injectées dans le prompt pré-construit).
5. La génération s'exécute sans sampler de grammaire (la grammaire a été désactivée en
   raison d'un problème SIGABRT dans llama.cpp avec les grammaires à déclenchement lazy
   sur les tokens multi-caractères). Le modèle est guidé vers une sortie JSON uniquement
   par le prompt de préambule et `/no_think`.
6. Après génération, `tools.ParseToolCall(output)` recherche le premier objet JSON équilibré
   correspondant à `{"name": "...", "arguments": {...}}`.
7. Si trouvé, la réponse contient `tool_calls` et `done_reason: "tool_calls"`.
   Sinon, la réponse est retournée comme contenu textuel ordinaire.

### 4.4 Sortie structurée (`format: "json"` ou JSON Schema)

1. Le client envoie `POST /api/chat` (ou `/api/generate`) avec `"format": "json"`
   ou `"format": <objet JSON Schema>`.
2. `resolveFormat` retourne `jsongrammar.JSONGrammar` (toujours la grammaire canonique
   `json.gbnf` de llama.cpp) et, pour les objets schéma, la map de schéma.
3. `injectNoThink` ajoute `/no_think` au dernier message utilisateur et injecte
   le prompt système JSON-only : *« Répondez UNIQUEMENT avec un objet JSON valide… »*.
   `</think>` et les tokens de fin de tour (`<|im_end|>`) sont ajoutés comme stop-strings.
4. Le moteur s'appuie actuellement sur l'ingénierie de prompt plutôt que sur les
   contraintes du sampler de grammaire (la grammaire a été retirée en raison de SIGABRT
   sur les états NFA vides). Le prompt système + `/no_think` guident le modèle à produire
   du JSON valide.
5. Après génération, si un JSON Schema était fourni, `jsongrammar.ValidateSchema`
   valide la réponse. En cas de non-correspondance, HTTP 422 est retourné.

### 4.5 Téléchargement de modèle (`POST /api/pull`)

1. **`api.Handler.Pull`** décode la `PullRequest` (nom/référence du modèle).
2. `models.Manager.Resolve` normalise la référence en clé de registre `(name, tag)`.
   `AliasLookup` consulte la table d'alias intégrée ; sinon `download.Resolve`
   construit l'URL de téléchargement HTTPS (gère les références raccourcies HuggingFace).
3. **`download.Get`** diffuse le fichier GGUF vers un chemin temporaire sous
   `$data-dir/tmp/`, calcule le condensé SHA-256 au fil de l'eau et émet des callbacks
   de progression. Le handler diffuse des lignes de progression NDJSON au client.
4. Une fois terminé, le blob est déplacé atomiquement (via `os.Rename`) vers
   `$data-dir/blobs/sha256-<digest>` (stockage adressé par contenu).
5. **`models.Manager.Save`** écrit un manifest JSON dans
   `$data-dir/manifests/<name>/<tag>.json` avec le condensé, la taille, l'URL source
   et les métadonnées extraites du GGUF.
6. Une dernière ligne NDJSON `{"status": "success", "digest": "sha256:<digest>"}` est
   envoyée au client.

---

## 5. Référence de configuration

La configuration est superposée : les défauts compilés sont remplacés par le fichier TOML,
puis par les variables d'environnement, puis par les flags CLI. Seuls les flags CLI
explicitement définis remplacent les sources de priorité inférieure.

| Flag CLI | Variable d'environnement | Clé TOML | Défaut | Description |
|---|---|---|---|---|
| `--host` | `BEEKET_HOST` | `[server] host` | `127.0.0.1` | Adresse d'écoute HTTP |
| `--port` | `BEEKET_PORT` | `[server] port` | `11435` | Port d'écoute HTTP |
| `--data-dir` | `BEEKET_DATA_DIR` | `[paths] data_dir` | `$XDG_DATA_HOME/beeket` | Répertoire de données racine (blobs, manifests) |
| `--lib-dir` | `BEEKET_LIB_DIR` | `[paths] lib_dir` | `<data-dir>/lib` | Répertoire contenant la bibliothèque partagée llama.cpp |
| `--backend` | `BEEKET_BACKEND` | `[runtime] backend` | `auto` | Backend de calcul : `auto`, `cpu`, `cuda`, `metal`, `vulkan`, `rocm` |
| `--gpu-layers` | `BEEKET_GPU_LAYERS` | `[runtime] gpu_layers` | `-1` (toutes) | Couches à décharger sur GPU ; `-1` signifie toutes |
| `--num-parallel` | `BEEKET_NUM_PARALLEL` | `[runtime] num_parallel` | `1` | Slots d'inférence parallèles par modèle |
| `--max-loaded-models` | `BEEKET_MAX_LOADED_MODELS` | `[runtime] max_loaded` | `3` | Nombre maximum de modèles simultanément en mémoire |
| `--keep-alive` | `BEEKET_KEEP_ALIVE` | `[runtime] keep_alive` | `5m` | Délai d'inactivité avant déchargement du modèle (durée Go, ex. `5m`, `1h`) |
| `--context-size` | `BEEKET_CONTEXT_SIZE` | `[runtime] context_size` | `4096` | Fenêtre de contexte par défaut en tokens |
| `--auto-install-lib` | `BEEKET_AUTO_INSTALL_LIB` | `[runtime] auto_install_lib` | `false` | Télécharger automatiquement la bibliothèque partagée llama.cpp au démarrage |
| `--lib-version` | _(aucune)_ | `[runtime] lib_version` | dernière | Version llama.cpp à installer (seulement avec `--auto-install-lib`) |
| `--lib-upgrade` | _(aucune)_ | `[runtime] lib_upgrade` | `false` | Forcer la réinstallation même si la bibliothèque est déjà présente |
| `--lib-install-timeout` | _(aucune)_ | _(aucune)_ | `10m` | Délai maximum pour le téléchargement de la bibliothèque |
| `--metrics-enabled` | `BEEKET_METRICS_ENABLED` | `[runtime] metrics_enabled` | `true` | Exposer les métriques Prometheus à `/metrics` |
| `--metrics-bind` | `BEEKET_METRICS_BIND` | `[runtime] metrics_bind` | _(désactivé)_ | Adresse secondaire pour `/metrics` (ex. `0.0.0.0:11436`) |
| `--log-level` | `BEEKET_LOG_LEVEL` | `[log] level` | `info` | Niveau de verbosité : `debug`, `info`, `warn`, `error` |
| `--log-format` | `BEEKET_LOG_FORMAT` | `[log] format` | `text` | Format des logs : `text` (lisible) ou `json` (structuré) |
| `--config` | _(aucune)_ | _(n/a)_ | `~/.config/beeket/beeket.toml` | Chemin vers le fichier de configuration TOML |

**Ordre de résolution du répertoire de bibliothèque** (pour `--lib-dir`) :

1. Flag `--lib-dir` / clé TOML `[paths] lib_dir`
2. Variable d'environnement `BEEKET_LIB_DIR`
3. Variable d'environnement `YZMA_LIB`
4. `<data-dir>/lib`

**Exemple de fichier de configuration TOML :**

```toml
[server]
host = "0.0.0.0"
port = 11435

[paths]
data_dir = "/var/lib/beeket"

[runtime]
backend       = "cuda"
gpu_layers    = -1
num_parallel  = 4
max_loaded    = 5
keep_alive    = "10m"
context_size  = 8192
metrics_enabled = true
metrics_bind  = "0.0.0.0:11436"

[log]
level  = "info"
format = "json"
```

---

## 6. Support des modèles pensants (Thinking Models)

Beeket supporte les modèles « pensants » (ex. Qwen3, QwQ, DeepSeek-R1) qui émettent
un raisonnement chaîne-de-pensée à l'intérieur de blocs `<think>…</think>` avant la
réponse finale.

### 6.1 Le mécanisme `/no_think`

Selon la [documentation Qwen3](https://qwen.readthedocs.io/), le token de contrôle
`/no_think` doit être **ajouté au dernier message utilisateur** pour supprimer le préambule
de raisonnement. Beeket l'implémente dans deux chemins de code :

- **`api.Handler.Generate`** — ajoute `/no_think` à `req.Prompt` quand
  `think: false` ou qu'une sortie structurée est demandée.
- **`api.Handler.Chat`** → **`injectNoThink`** — parcourt `effectiveMsgs` depuis la fin,
  trouve le dernier message de rôle `user`, et ajoute `/no_think` s'il n'est pas déjà présent.
  Un garde `strings.HasSuffix` prévient la double injection.

### 6.2 Quand la pensée est supprimée

La suppression de la pensée est déclenchée automatiquement dans trois situations :

| Déclencheur | Condition |
|---|---|
| Désactivation explicite | `"think": false` dans le corps de la requête |
| Sortie structurée | `"format": "json"` ou `"format": <JSON Schema>` |
| Appel d'outil | `"tools": [...]` présent dans le corps de la requête |

### 6.3 Filet de sécurité

Même avec `/no_think`, un modèle peut tout de même émettre un bloc `<think>`. Beeket
ajoute `"</think>"` comme stop-string dès que la pensée est supprimée, de sorte que la
génération s'arrête immédiatement si un bloc de raisonnement apparaît. Pour la sortie
structurée, les tokens de fin de tour (`<|im_end|>`, `<|im_start|>`) sont également
ajoutés comme stop-strings pour éviter les fuites de contexte.

### 6.4 Support du template de chat natif

Pour les requêtes sans appel d'outil, `engine.Session.Generate` accepte une slice
`opts.Messages` et appelle `llama.ChatApplyTemplate` avec **le nom du template propre au
modèle** (lu dans l'en-tête GGUF). Ceci garantit que les variables de template spécifiques
aux modèles pensants (ex. `enable_thinking`) sont rendues correctement.

---

## 7. Points d'extension

Beeket est conçu de sorte que les extensions courantes ne nécessitent des modifications
que dans un ou deux packages.

### 7.1 Ajouter un nouvel endpoint HTTP

1. Ajouter une méthode handler sur `api.Handler` dans `internal/api/handlers.go` :
   ```go
   func (h *Handler) MonEndpoint(w http.ResponseWriter, r *http.Request) { … }
   ```
2. Enregistrer la route dans `api.NewServer` (`internal/api/server.go`) :
   ```go
   s.mux.HandleFunc("POST /api/mon-endpoint", h.MonEndpoint)
   ```
   Le serveur utilise la syntaxe de pattern `method path` de Go 1.22.

### 7.2 Remplacer le scheduler (modèle de concurrence alternatif)

`api.Handler` dépend de deux petites interfaces, et non du type concret `*scheduler.Scheduler` :

```go
type generatorScheduler interface {
    Generate(ctx context.Context, name, tag, prompt string,
             opts engine.GenerateOptions, out func(string) error) error
    LoadedModels() []scheduler.LoadedInfo
}

type embedScheduler interface {
    Embed(ctx context.Context, name, tag, input string) ([]float32, int, error)
}
```

Fournir une struct qui satisfait ces deux interfaces et la passer à `api.NewHandlerWithConfig`.

### 7.3 Ajouter une nouvelle option de sampler

1. Ajouter le champ à `engine.SamplerOptions` dans `internal/engine/engine.go`.
2. Le câbler dans `buildSampler` (et `buildSamplerWithGrammar`) en appelant la fonction
   `llama.SamplerChainAdd` appropriée.
3. L'exposer dans le type de requête `Options` dans `internal/api/types.go`.
4. Le mapper dans `buildGenerateOptions` dans `internal/api/handlers.go`.
5. Le documenter dans `docs/options.md`.

### 7.4 Ajouter une nouvelle grammaire pour sortie structurée

1. Créer un nouveau package à côté d'`internal/jsongrammar/` avec votre constante de grammaire.
2. Étendre `resolveFormat` dans `internal/api/handlers.go` pour reconnaître une nouvelle
   valeur `format` et retourner votre chaîne de grammaire.
3. Si une validation post-génération est nécessaire, ajouter une fonction `Validate` similaire
   à `jsongrammar.ValidateSchema`.

### 7.5 Ajouter un nouveau dialecte d'appel d'outil

Le pipeline d'outils est divisé en trois fonctions dans `internal/tools/` :

- `RenderToolPreface(tools)` — construit l'injection dans le prompt système.
- `RewriteToolMessages(messages)` — normalise les messages de rôle `tool`.
- `ParseToolCall(output)` — extrait le JSON structuré de la sortie du modèle.

Remplacer l'une d'elles individuellement pour modifier la façon dont les outils sont
présentés au modèle ou dont la réponse est interprétée.

### 7.6 Ajouter un nouveau backend de calcul

1. Étendre `libinstall.Ensure` dans `internal/libinstall/` pour détecter et télécharger
   la bibliothèque partagée du nouveau backend.
2. Ajouter le nom du backend à la map `validBackends` de `config.Validate`.
3. Le package `internal/engine` est entièrement agnostique au backend — il passe
   le chemin de la bibliothèque à `llama.Load` et laisse Yzma/llama.cpp sélectionner le backend.

---

## 8. Glossaire

| Terme | Définition |
|---|---|
| **GGUF** | *GGML Unified Format* — format de fichier utilisé par llama.cpp pour stocker les poids quantisés du modèle, le vocabulaire et les métadonnées (y compris les templates de chat) dans un unique fichier binaire. |
| **GBNF** | *GGML BNF* — format de grammaire de type BNF compris par le sampler de grammaire de llama.cpp. Utilisé pour contraindre la génération de tokens à des productions valides (ex. objets JSON). |
| **Sampler** | Le composant qui sélectionne le prochain token à partir de la distribution de probabilité produite par le modèle. Beeket supporte des chaînes de samplers : TopK → TopP / TypicalP → MinP → Température → (Grammaire) → Distribution. |
| **Contexte / cache KV** | Le cache clé-valeur maintenu par llama.cpp pour chaque session d'inférence. Stocke les clés et valeurs d'attention pour tous les tokens précédemment traités. Taille définie par `--context-size`. |
| **Session** | Un `engine.Session` est un contexte d'inférence llama.cpp associé à un modèle chargé. Beeket crée une session par `Worker` ; chaque requête réutilise la même session. |
| **Worker** | Un `scheduler.Worker` possède un `engine.Session` et une goroutine. Il traite les requêtes d'inférence séquentiellement depuis un canal de 32 emplacements. |
| **EmbedWorker** | Un `scheduler.EmbedWorker` possède un `engine.EmbedSession` dédié à l'extraction d'embeddings. Indexé séparément des workers de génération pour ne pas les déplacer. |
| **Keep-alive** | Le délai d'inactivité après lequel un modèle inutilisé est évincé de la mémoire. Configurable via `--keep-alive` (défaut `5m`). Une boucle d'éviction s'exécute toutes les 30 secondes. |
| **Manifest** | Un petit fichier JSON (`$data-dir/manifests/<name>/<tag>.json`) qui enregistre le condensé du modèle, sa taille, l'URL source et ses métadonnées. L'équivalent sur disque d'un tag de registre. |
| **Blob** | Le fichier GGUF brut stocké de façon adressée par contenu dans `$data-dir/blobs/sha256-<condensé-hex>`. |
| **Backend** | Le backend d'accélération matérielle utilisé par llama.cpp : `cpu`, `cuda`, `metal`, `vulkan`, ou `rocm`. Sélectionné au démarrage ; `auto` laisse Yzma détecter la meilleure option disponible. |
| **Pooling** | La stratégie utilisée pour agréger les embeddings par token en un vecteur unique pour une séquence. Beeket utilise `GetEmbeddingsSeq` (pooling de séquence) plutôt que des embeddings par token. |
| **Normalisation L2** | Division d'un vecteur d'embedding par sa norme euclidienne (L2) afin que tous les vecteurs se trouvent sur la sphère unité. Permet de calculer la similarité cosinus comme un simple produit scalaire. |
| **Yzma** | L'enveloppe Go pure sans CGo (`hybridgroup/yzma`, utilise `purego` et `jupiterrider/ffi`) utilisée par Beeket pour appeler l'API C de llama.cpp. Yzma charge la bibliothèque partagée à l'exécution via `llama.Load` plutôt que de la lier statiquement. |

---

## 9. Références croisées

| Sujet | Document |
|---|---|
| Spécification API (routes, formats des requêtes/réponses) | [spec-v0.1.md](./spec-v0.1.md) |
| Installation et configuration | [SETUP.md](./SETUP.md) |
| Téléchargement de modèles (formats d'URL, références HuggingFace) | [models.md](./models.md) |
| Référence des options de sampling | [options.md](./options.md) |
| Guide de sortie structurée | [structured-output.md](./structured-output.md) |
| Guide des embeddings | [embeddings.md](./embeddings.md) |
| Guide des appels d'outils | [tools.md](./tools.md) |
| Monitoring et métriques | [monitoring.md](./monitoring.md) |
