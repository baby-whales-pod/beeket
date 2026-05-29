# Débuter avec yzma : inférence LLM locale en Go

[yzma](https://github.com/hybridgroup/yzma) est une bibliothèque Go qui fournit des
liaisons vers [llama.cpp](https://github.com/ggml-org/llama.cpp), permettant d'exécuter
des modèles de langage de grande et petite taille **localement, dans votre propre
processus Go** — sans CGo, sans serveur externe, et avec accélération matérielle
complète (CUDA, Metal, Vulkan, ROCm, …).

Ce tutoriel vous guide de zéro jusqu'à un programme de chat en streaming fonctionnel.

---

## Table des matières

1. [Ce que fait yzma](#1-ce-que-fait-yzma)
2. [Prérequis](#2-prérequis)
3. [Créer un nouveau projet Go](#3-créer-un-nouveau-projet-go)
4. [Ajouter la dépendance yzma](#4-ajouter-la-dépendance-yzma)
5. [Installer les bibliothèques partagées llama.cpp](#5-installer-les-bibliothèques-partagées-llamacpp)
6. [Télécharger un modèle](#6-télécharger-un-modèle)
7. [Charger la bibliothèque et initialiser llama.cpp](#7-charger-la-bibliothèque-et-initialiser-llamacpp)
8. [Charger un fichier de modèle](#8-charger-un-fichier-de-modèle)
9. [Créer un contexte d'inférence](#9-créer-un-contexte-dinférence)
10. [Construire un prompt de chat](#10-construire-un-prompt-de-chat)
11. [Tokeniser le prompt](#11-tokeniser-le-prompt)
12. [Configurer une chaîne de samplers](#12-configurer-une-chaîne-de-samplers)
13. [Exécuter l'inférence token par token](#13-exécuter-linférence-token-par-token)
14. [Décoder les tokens en texte](#14-décoder-les-tokens-en-texte)
15. [Libérer les ressources](#15-libérer-les-ressources)
16. [Exemple complet fonctionnel](#16-exemple-complet-fonctionnel)

---

## 1. Ce que fait yzma

yzma utilise les paquets [`purego`](https://github.com/ebitengine/purego) et
[`ffi`](https://github.com/JupiterRider/ffi) pour appeler la bibliothèque partagée
`llama.cpp` **depuis le même processus OS**. Cela offre des performances quasi-natives
sans compilateur C, sans CGo, et sans serveur de modèle séparé.

Caractéristiques principales :

- Compilation avec un simple `go build` / `go run` — aucune chaîne d'outils C requise.
- Compatible avec tous les modèles au format GGUF (le format standard pour les LLMs quantisés sur Hugging Face).
- Supporte les backends CPU, CUDA, Metal, Vulkan, HIP/ROCm, SYCL et OpenCL.
- Suit les versions de llama.cpp de près, avec tests automatiques à chaque nouvelle version.

---

## 2. Prérequis

| Requis | Notes |
|---|---|
| Go 1.25+ | yzma v1.13.0 nécessite Go 1.25.0 ou une version ultérieure. |
| CLI `yzma` | Utilisé pour installer les bibliothèques llama.cpp et télécharger des modèles. |
| Bibliothèques partagées llama.cpp | Installées via `yzma install`. |

### Installer le CLI yzma

```bash
go install github.com/hybridgroup/yzma@latest
```

Assurez-vous que `$(go env GOPATH)/bin` est dans votre `PATH`.

---

## 3. Créer un nouveau projet Go

```bash
mkdir mon-app-llm
cd mon-app-llm
go mod init github.com/votrenom/mon-app-llm
```

---

## 4. Ajouter la dépendance yzma

```bash
go get github.com/hybridgroup/yzma@latest
```

Votre `go.mod` référencera désormais `github.com/hybridgroup/yzma`.

---

## 5. Installer les bibliothèques partagées llama.cpp

yzma dépend de la **bibliothèque partagée** `llama.cpp` au moment de l'exécution.
Utilisez le CLI `yzma` pour télécharger un binaire pré-compilé pour votre plateforme.

### Choisir un répertoire et installer

```bash
yzma install --lib /chemin/vers/lib
```

Indiquez à yzma où trouver la bibliothèque au moment de l'exécution via la variable
d'environnement `YZMA_LIB` :

```bash
export YZMA_LIB=/chemin/vers/lib
```

Dans votre programme Go, lisez cette variable et passez-la à `llama.Load` :

```go
libPath := os.Getenv("YZMA_LIB")
if err := llama.Load(libPath); err != nil {
    log.Fatal(err)
}
```

Passer une chaîne vide indique à yzma d'utiliser le chemin de recherche du linker
dynamique du système d'exploitation.

### Variantes GPU

| Plateforme | Commande |
|---|---|
| CPU seulement (défaut) | `yzma install --lib /chemin/vers/lib` |
| CUDA (Linux / Windows) | `yzma install --lib /chemin/vers/lib --processor cuda` |
| ROCm (Linux / GPU AMD) | `yzma install --lib /chemin/vers/lib --processor rocm` |
| Vulkan | `yzma install --lib /chemin/vers/lib --processor vulkan` |
| Metal (macOS) | `yzma install --lib /chemin/vers/lib` (détection automatique) |

> Suivez les instructions supplémentaires affichées dans le terminal après `yzma install`
> (par exemple, exécuter `ldconfig` sous Linux).

---

## 6. Télécharger un modèle

Les modèles doivent être au format **GGUF**. Ce tutoriel utilise
`SmolLM2-135M.Q4_K_M.gguf` — un modèle compact mais fonctionnel qui se télécharge en
quelques secondes et fonctionne sur n'importe quel matériel :

```bash
yzma model get -u https://huggingface.co/QuantFactory/SmolLM2-135M-GGUF/resolve/main/SmolLM2-135M.Q4_K_M.gguf
```

Par défaut, les modèles sont stockés dans `~/models/`. La fonction utilitaire
`download.DefaultModelsDir()` (du paquet `github.com/hybridgroup/yzma/pkg/download`)
retourne ce chemin.

Pour un modèle plus grand et optimisé pour le suivi d'instructions, remplacez l'URL par
une provenant de <https://huggingface.co/models?library=gguf>.

---

## 7. Charger la bibliothèque et initialiser llama.cpp

Créez `main.go` et commencez par la séquence d'initialisation. Tout programme yzma doit :

1. Appeler `llama.Load` pour ouvrir la bibliothèque partagée.
2. Optionnellement, silencer la sortie de log de llama.cpp.
3. Appeler `llama.Init` pour initialiser les backends GGML.

```go
package main

import (
    "log"
    "os"

    "github.com/hybridgroup/yzma/pkg/llama"
)

func main() {
    // 1. Charger la bibliothèque partagée llama.cpp.
    //    YZMA_LIB doit pointer vers le répertoire contenant libllama.so / llama.dll / libllama.dylib.
    //    Une chaîne vide indique à yzma d'utiliser le chemin de recherche du linker OS.
    libPath := os.Getenv("YZMA_LIB")
    if err := llama.Load(libPath); err != nil {
        log.Fatalf("llama.Load : %v", err)
    }

    // 2. Silencer la sortie de log verbeuse de llama.cpp (optionnel mais recommandé).
    llama.LogSet(llama.LogSilent())

    // 3. Initialiser les backends GGML (CPU, CUDA, Metal, …).
    llama.Init()
    defer llama.Close()

    // … reste du programme
}
```

> `llama.Init()` et `llama.Close()` ne retournent pas d'erreur. L'acquisition de
> ressources se fait via les fonctions de modèle et de contexte qui, elles, retournent
> des erreurs.

---

## 8. Charger un fichier de modèle

```go
import (
    "log"
    "path/filepath"

    "github.com/hybridgroup/yzma/pkg/download"
    "github.com/hybridgroup/yzma/pkg/llama"
)

const modelFile = "SmolLM2-135M.Q4_K_M.gguf"

// ModelDefaultParams retourne des valeurs par défaut sensées :
//   - NGpuLayers = 0  (CPU seulement ; mettre 99 pour tout décharger sur le GPU)
//   - UseMmap    = true
//   - UseMlock   = false
modelParams := llama.ModelDefaultParams()
// Décommentez pour utiliser le GPU :
// modelParams.NGpuLayers = 99

modelPath := filepath.Join(download.DefaultModelsDir(), modelFile)
model, err := llama.ModelLoadFromFile(modelPath, modelParams)
if err != nil {
    log.Fatalf("ModelLoadFromFile : %v", err)
}
// À l'intérieur d'une fonction — defer s'exécute au retour de la fonction
defer func() { _ = llama.ModelFree(model) }()
```

`ModelLoadFromFile` retourne un handle opaque `llama.Model` (un `uintptr` en interne).
Elle retourne une erreur si le fichier n'existe pas ou n'est pas un GGUF valide.

---

## 9. Créer un contexte d'inférence

Le **contexte** contient le cache KV et gère l'état du décodage. Il est créé à partir
d'un modèle chargé :

```go
ctxParams := llama.ContextDefaultParams()
// Surcharger la taille de la fenêtre de contexte si nécessaire (0 = utiliser le défaut du modèle) :
// ctxParams.NCtx = 2048

ctx, err := llama.InitFromModel(model, ctxParams)
if err != nil {
    log.Fatalf("InitFromModel : %v", err)
}
defer func() { _ = llama.Free(ctx) }()
```

Champs clés de `ContextParams` :

| Champ | Défaut | Signification |
|---|---|---|
| `NCtx` | depuis le modèle | Nombre de tokens dans le cache KV |
| `NBatch` | 2048 | Taille maximale de lot logique |
| `NThreads` | runtime.NumCPU() | Threads pour le décodage token par token |
| `NThreadsBatch` | runtime.NumCPU() | Threads pour le traitement du prompt |

---

## 10. Construire un prompt de chat

Les modèles optimisés pour les instructions attendent un format de chat spécifique
(ChatML, Llama-3, Qwen, …). yzma encapsule l'API de templating de llama.cpp pour que
vous n'ayez pas à formater les chaînes manuellement.

```go
// Obtenir le handle du vocabulaire — nécessaire pour la tokenisation et le templating.
vocab := llama.ModelGetVocab(model)

// Construire la liste des messages de chat.
messages := []llama.ChatMessage{
    llama.NewChatMessage("system", "Tu es un assistant serviable."),
    llama.NewChatMessage("user", "Quelle est la capitale de la France ?"),
}

// Appliquer le template de chat embarqué dans le modèle.
// Passer "" comme nom de template pour utiliser le template propre au modèle.
// Mettre addAssistantPrompt = true pour que le modèle sache qu'il doit répondre.
buf := make([]byte, 4096)
n := llama.ChatApplyTemplate("", messages, true, buf)
if n <= 0 {
    log.Fatal("ChatApplyTemplate a échoué")
}
formattedPrompt := string(buf[:n])
```

Paramètres de `ChatApplyTemplate` :

| Paramètre | Description |
|---|---|
| `template` | Template nommé (`"chatml"`, `"llama2"`, …) ou `""` pour utiliser celui du modèle |
| `chat` | Slice de `ChatMessage` |
| `addAssistantPrompt` | Ajoute le marqueur d'ouverture de l'assistant si `true` |
| `buf` | Buffer de sortie — doit être assez grand pour le prompt formaté |

---

## 11. Tokeniser le prompt

```go
// Tokenize convertit le prompt formaté en une slice d'identifiants de tokens entiers.
// addSpecial=true  → ajouter le token BOS en début
// parseSpecial=true → reconnaître les tokens de contrôle <|...|> dans le texte
tokens := llama.Tokenize(vocab, formattedPrompt, true, true)
if len(tokens) == 0 {
    log.Fatal("la tokenisation n'a produit aucun token")
}
fmt.Printf("Prompt : %d tokens\n", len(tokens))
```

---

## 12. Configurer une chaîne de samplers

Les samplers se situent entre les logits bruts du modèle et le choix final du token.
On construit une **chaîne** ; chaque sampler filtre ou re-pondère la distribution de
probabilité avant que le suivant ne la traite.

```go
// Créer une chaîne vide.
sampler := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
defer llama.SamplerFree(sampler) // libère la chaîne ET tous les samplers ajoutés

// Ajouter les samplers dans l'ordre :

// 1. Top-K : ne conserver que les K tokens les plus probables.
llama.SamplerChainAdd(sampler, llama.SamplerInitTopK(40))

// 2. Top-P (échantillonnage noyau) : conserver le plus petit ensemble de tokens
//    dont la probabilité cumulée dépasse P.  Le second arg est min_keep (1 = garder
//    au moins un token même si P est très petit).
llama.SamplerChainAdd(sampler, llama.SamplerInitTopP(0.95, 1))

// 3. Température étendue : t=0.8 (plus bas = plus déterministe),
//    delta=0.0 et exponent=1.0 donnent une température simple (sans plage dynamique).
llama.SamplerChainAdd(sampler, llama.SamplerInitTempExt(0.8, 0.0, 1.0))

// 4. Distribution : tire le token final depuis la distribution filtrée.
//    llama.DefaultSeed (0xFFFFFFFF) génère une nouvelle graine aléatoire à chaque exécution.
llama.SamplerChainAdd(sampler, llama.SamplerInitDist(llama.DefaultSeed))
```

> **Échantillonnage glouton** (toujours choisir le token le plus probable, entièrement
> déterministe) :
>
> ```go
> sampler := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
> llama.SamplerChainAdd(sampler, llama.SamplerInitGreedy())
> ```

---

## 13. Exécuter l'inférence token par token

La boucle de génération yzma effectue trois étapes par token :

1. **`llama.Decode`** — exécute le passage avant du transformer sur un lot, remplit le
   cache KV et calcule les logits.
2. **`llama.SamplerSample`** — choisit le prochain token à partir des logits.
3. **`llama.SamplerAccept`** — informe le sampler du token choisi (nécessaire pour les
   samplers de pénalité de répétition et similaires).

```go
const maxNouveauxTokens = 200

// Encapsuler les tokens du prompt dans un lot à séquence unique.
batch := llama.BatchGetOne(tokens)

for range maxNouveauxTokens {
    // Exécuter le passage avant.
    if _, err := llama.Decode(ctx, batch); err != nil {
        log.Fatalf("Decode : %v", err)
    }

    // Échantillonner le prochain token. idx=-1 signifie "utiliser les logits du dernier token".
    token := llama.SamplerSample(sampler, ctx, -1)

    // Arrêter à tout token de fin de génération (EOS, EOT, etc.).
    if llama.VocabIsEOG(vocab, token) {
        break
    }

    // Convertir l'identifiant de token en fragment de texte et le diffuser.
    piece := make([]byte, 64)
    n := llama.TokenToPiece(vocab, token, piece, 0, true)
    if n > 0 {
        fmt.Print(string(piece[:n]))
    }

    // Informer le sampler du token accepté.
    llama.SamplerAccept(sampler, token)

    // Fournir le nouveau token comme prochain lot d'un seul token.
    batch = llama.BatchGetOne([]llama.Token{token})
}
fmt.Println()
```

---

## 14. Décoder les tokens en texte

`llama.TokenToPiece` convertit un identifiant de token en fragment d'octets UTF-8 :

```go
piece := make([]byte, 64) // 64 octets suffisent pour n'importe quel token unique
n := llama.TokenToPiece(vocab, token, piece, 0, true)
// n > 0 : octets écrits ; n < 0 : buffer trop petit (augmenter la taille de la slice)
fmt.Print(string(piece[:n]))
```

| Paramètre | Description |
|---|---|
| `vocab` | Handle du vocabulaire depuis `llama.ModelGetVocab` |
| `token` | Identifiant de token à convertir |
| `buf` | Slice d'octets de sortie |
| `lstrip` | Espaces de début à supprimer (passer `0`) |
| `special` | Afficher les tokens spéciaux comme texte (`true` est sûr pour l'affichage) |

---

## 15. Libérer les ressources

Libérez toujours les ressources dans l'ordre inverse de leur création :

```go
llama.SamplerFree(sampler)              // libère la chaîne + tous les samplers qu'elle contient
_ = llama.Free(ctx)                    // libère le contexte d'inférence et le cache KV
_ = llama.ModelFree(model)             // décharge les poids du modèle
llama.Close()                          // arrête les backends GGML
```

Utiliser `defer` au moment de la création est l'approche idiomatique en Go (comme
montré dans l'exemple complet ci-dessous).

---

## 16. Exemple complet fonctionnel

Le programme ci-dessous rassemble toutes les étapes. Il charge
`SmolLM2-135M.Q4_K_M.gguf`, formate un message utilisateur avec le template de chat
du modèle, et diffuse la réponse token par token sur stdout.

```go
package main

import (
    "flag"
    "fmt"
    "log"
    "os"
    "path/filepath"

    "github.com/hybridgroup/yzma/pkg/download"
    "github.com/hybridgroup/yzma/pkg/llama"
)

func main() {
    // ---- flags ----
    modelName := flag.String("model", "SmolLM2-135M.Q4_K_M.gguf",
        "Nom du fichier GGUF dans ~/models/")
    userPrompt := flag.String("prompt", "Quelle est la capitale de la France ?",
        "Message utilisateur à envoyer au modèle")
    maxTokens := flag.Int("max", 200, "Nombre maximal de tokens à générer")
    flag.Parse()

    // ---- 1. Charger la bibliothèque partagée llama.cpp ----
    libPath := os.Getenv("YZMA_LIB")
    if err := llama.Load(libPath); err != nil {
        log.Fatalf("llama.Load : %v", err)
    }

    // ---- 2. Silencer les logs verbeux de llama.cpp ----
    llama.LogSet(llama.LogSilent())

    // ---- 3. Initialiser les backends GGML ----
    llama.Init()
    defer llama.Close()

    // ---- 4. Charger le modèle ----
    modelPath := filepath.Join(download.DefaultModelsDir(), *modelName)
    modelParams := llama.ModelDefaultParams()
    // Pour décharger toutes les couches sur le GPU :  modelParams.NGpuLayers = 99

    model, err := llama.ModelLoadFromFile(modelPath, modelParams)
    if err != nil {
        log.Fatalf("ModelLoadFromFile : %v", err)
    }
    defer func() { _ = llama.ModelFree(model) }()

    // ---- 5. Créer le contexte d'inférence ----
    ctxParams := llama.ContextDefaultParams()
    ctx, err := llama.InitFromModel(model, ctxParams)
    if err != nil {
        log.Fatalf("InitFromModel : %v", err)
    }
    defer func() { _ = llama.Free(ctx) }()

    // ---- 6. Construire le prompt de chat ----
    vocab := llama.ModelGetVocab(model)

    messages := []llama.ChatMessage{
        llama.NewChatMessage("system", "Tu es un assistant serviable."),
        llama.NewChatMessage("user", *userPrompt),
    }

    tmplBuf := make([]byte, 8192)
    n := llama.ChatApplyTemplate("", messages, true, tmplBuf)
    if n <= 0 {
        log.Fatal("ChatApplyTemplate a échoué")
    }
    formattedPrompt := string(tmplBuf[:n])

    // ---- 7. Tokeniser ----
    tokens := llama.Tokenize(vocab, formattedPrompt, true, true)
    if len(tokens) == 0 {
        log.Fatal("la tokenisation n'a produit aucun token")
    }

    // ---- 8. Configurer la chaîne de samplers ----
    sampler := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
    defer llama.SamplerFree(sampler)

    llama.SamplerChainAdd(sampler, llama.SamplerInitTopK(40))
    llama.SamplerChainAdd(sampler, llama.SamplerInitTopP(0.95, 1))
    llama.SamplerChainAdd(sampler, llama.SamplerInitTempExt(0.8, 0.0, 1.0))
    llama.SamplerChainAdd(sampler, llama.SamplerInitDist(llama.DefaultSeed))

    // ---- 9. Boucle d'inférence ----
    fmt.Printf("\nUtilisateur : %s\n\nAssistant : ", *userPrompt)

    batch := llama.BatchGetOne(tokens)
    for range *maxTokens {
        if _, err := llama.Decode(ctx, batch); err != nil {
            log.Fatalf("Decode : %v", err)
        }

        token := llama.SamplerSample(sampler, ctx, -1)

        if llama.VocabIsEOG(vocab, token) {
            break
        }

        piece := make([]byte, 64)
        n := llama.TokenToPiece(vocab, token, piece, 0, true)
        if n > 0 {
            fmt.Print(string(piece[:n]))
        }

        llama.SamplerAccept(sampler, token)
        batch = llama.BatchGetOne([]llama.Token{token})
    }

    fmt.Println()
}
```

### Structure du projet

```
mon-app-llm/
├── go.mod
├── go.sum
└── main.go
```

### Télécharger le modèle et exécuter

```bash
# Télécharger le modèle (une seule fois suffit)
yzma model get -u https://huggingface.co/QuantFactory/SmolLM2-135M-GGUF/resolve/main/SmolLM2-135M.Q4_K_M.gguf

# Exécuter
YZMA_LIB=/chemin/vers/lib go run ./main.go

# Avec un prompt personnalisé
YZMA_LIB=/chemin/vers/lib go run ./main.go -prompt "Explique ce qu'est une goroutine."

# Déchargement GPU (si vous avez installé les bibliothèques CUDA)
# (définir modelParams.NGpuLayers = 99 dans le code source)
YZMA_LIB=/chemin/vers/lib go run ./main.go
```

Sortie attendue (le texte exact varie avec l'échantillonnage par température) :

```
Utilisateur : Quelle est la capitale de la France ?

Assistant : Paris est la capitale de la France.
```

### Résolution des problèmes

| Problème | Solution |
|---|---|
| `unable to load library` | Définir `YZMA_LIB` vers le répertoire contenant le fichier `.so` / `.dylib` / `.dll`. |
| `context size too large` | Réduire `ctxParams.NCtx`. |
| Mémoire insuffisante | Utiliser un modèle plus quantisé (Q4_K_M au lieu de F16), réduire `NCtx`. |
| Inférence lente | Installer la variante CUDA / Metal / Vulkan des bibliothèques. |
| `failed to load model` | Vérifier le chemin du fichier et que le fichier est un GGUF valide. |
