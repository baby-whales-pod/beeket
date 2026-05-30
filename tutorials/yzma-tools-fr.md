# Appels d'outils (_tool calling_) avec yzma

yzma n'a pas de support natif du _tool calling_ — il n'y a pas d'API dédiée aux
fonctions/outils. Tout se fait par **ingénierie de prompt** : on décrit les outils
disponibles dans le message système, on demande au modèle de répondre dans un format
JSON strict, et on utilise optionnellement une grammaire GBNF pour contraindre la
génération.

---

## Prérequis

| Requis | Version / Note |
|---|---|
| Go | 1.25+ |
| yzma | v1.13.0 |
| Bibliothèques llama.cpp | Installées via `yzma install` |
| Modèle | Qwen3 ou Llama-3 Instruct (modèles qui suivent bien les formats JSON) |

> Les petits modèles (< 1 B de paramètres) ont tendance à ne pas respecter le format.
> Privilégier **Qwen3-1.7B-Q4_K_M** ou **Llama-3.2-3B-Instruct-Q4_K_M**.

---

## Principe général

```
┌─────────────────────────────────────────────────────┐
│  Message système                                    │
│  ┌─────────────────────────────────────────────┐   │
│  │ Outils disponibles (JSON schema)            │   │
│  │ Instruction : répondre en JSON uniquement   │   │
│  └─────────────────────────────────────────────┘   │
│  Message utilisateur → question en langage naturel │
│                                                     │
│  Sortie du modèle → JSON {"tool": ..., "args": ...}│
│                          ↓                          │
│  Code Go → parse JSON → appel de fonction Go       │
└─────────────────────────────────────────────────────┘
```

---

## Étape 1 — Définir les outils dans le prompt système

```go
const systemPrompt = `Tu es un assistant qui peut utiliser des outils.

Outils disponibles :
[
  {
    "name": "get_weather",
    "description": "Retourne la météo actuelle pour une ville.",
    "parameters": {
      "city": {"type": "string", "description": "Nom de la ville"}
    }
  }
]

Règles :
- Si tu dois utiliser un outil, réponds UNIQUEMENT avec un objet JSON valide.
- Format obligatoire : {"tool": "<nom>", "args": {<paramètres>}}
- N'ajoute aucun texte avant ou après le JSON.
- Si tu peux répondre sans outil, réponds normalement en français.`
```

---

## Étape 2 — Contraindre la sortie avec une grammaire GBNF (optionnel mais recommandé)

La grammaire GBNF force le modèle à produire du JSON valide dès le premier token,
sans possibilité de générer du texte libre :

```go
// Grammaire minimale : objet JSON avec "tool" (string) et "args" (objet)
const toolCallGrammar = `
root   ::= "{" ws "\"tool\"" ws ":" ws string ws "," ws "\"args\"" ws ":" ws object ws "}"
object ::= "{" ws (pair ("," ws pair)*)? ws "}"
pair   ::= string ws ":" ws value
value  ::= string | number | "true" | "false" | "null" | object | array
array  ::= "[" ws (value ("," ws value)*)? ws "]"
string ::= "\"" ([^"\\] | "\\" .)* "\""
number ::= "-"? ([0-9] | [1-9][0-9]*) ("." [0-9]+)? (([eE] [+-]? [0-9]+))?
ws     ::= [ \t\n]*
`
```

Ajouter le sampler de grammaire dans la chaîne :

```go
vocab := llama.ModelGetVocab(model)
grammarSampler := llama.SamplerInitGrammar(vocab, toolCallGrammar, "root")
llama.SamplerChainAdd(sampler, grammarSampler)
```

> **Attention :** si le modèle génère du texte avant le JSON (prose, pensées),
> le sampler de grammaire peut produire une erreur `SIGABRT`. Utilisez
> `SamplerInitGrammarLazyPatterns` (voir section avancée) ou assurez-vous que le
> modèle commence bien par `{`.

---

## Exemple complet

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/hybridgroup/yzma/pkg/download"
	"github.com/hybridgroup/yzma/pkg/llama"
)

// ToolCall représente l'appel d'outil généré par le modèle.
type ToolCall struct {
	Tool string            `json:"tool"`
	Args map[string]string `json:"args"`
}

const modelFile = "Qwen3-1.7B-Q4_K_M.gguf"

const systemPrompt = `Tu es un assistant qui peut utiliser des outils.

Outils disponibles :
[
  {
    "name": "get_weather",
    "description": "Retourne la météo actuelle pour une ville.",
    "parameters": {
      "city": {"type": "string", "description": "Nom de la ville"}
    }
  }
]

Règles strictes :
- Réponds UNIQUEMENT avec un objet JSON valide, sans texte supplémentaire.
- Format : {"tool": "<nom>", "args": {<paramètres>}}
- N'ajoute ni guillemets markdown, ni explication.`

// Grammaire GBNF pour contraindre la sortie à un appel d'outil JSON.
const toolCallGrammar = `root   ::= "{" ws "\"tool\"" ws ":" ws string ws "," ws "\"args\"" ws ":" ws object ws "}"
object ::= "{" ws (pair ("," ws pair)*)? ws "}"
pair   ::= string ws ":" ws value
value  ::= string | number | "true" | "false" | "null" | object | array
array  ::= "[" ws (value ("," ws value)*)? ws "]"
string ::= "\"" ([^"\\] | "\\" .)* "\""
number ::= "-"? ([0-9] | [1-9][0-9]*) ("." [0-9]+)? (([eE] [+-]? [0-9]+))?
ws     ::= [ \t\n]*
`

func main() {
	userQuestion := "Quel temps fait-il à Lyon en ce moment ?"

	// ---- 1. Initialisation ----
	libPath := os.Getenv("YZMA_LIB")
	if err := llama.Load(libPath); err != nil {
		log.Fatalf("llama.Load : %v", err)
	}
	llama.LogSet(llama.LogSilent())
	llama.Init()
	defer llama.Close()

	// ---- 2. Modèle et contexte ----
	modelPath := filepath.Join(download.DefaultModelsDir(), modelFile)
	model, err := llama.ModelLoadFromFile(modelPath, llama.ModelDefaultParams())
	if err != nil {
		log.Fatalf("ModelLoadFromFile : %v", err)
	}
	defer func() { _ = llama.ModelFree(model) }()

	ctx, err := llama.InitFromModel(model, llama.ContextDefaultParams())
	if err != nil {
		log.Fatalf("InitFromModel : %v", err)
	}
	defer func() { _ = llama.Free(ctx) }()

	vocab := llama.ModelGetVocab(model)

	// ---- 3. Prompt avec message système ----
	messages := []llama.ChatMessage{
		llama.NewChatMessage("system", systemPrompt),
		// Pour Qwen3 : /no_think désactive le bloc <think>...</think>
		llama.NewChatMessage("user", userQuestion+" /no_think"),
	}

	tmplBuf := make([]byte, 8192)
	n := llama.ChatApplyTemplate("", messages, true, tmplBuf)
	if n <= 0 {
		log.Fatal("ChatApplyTemplate a échoué")
	}

	// ---- 4. Tokenisation ----
	tokens := llama.Tokenize(vocab, string(tmplBuf[:n]), true, true)

	// ---- 5. Chaîne de samplers avec grammaire GBNF ----
	sampler := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
	defer llama.SamplerFree(sampler)

	// Grammaire en premier pour contraindre les logits dès le début
	llama.SamplerChainAdd(sampler, llama.SamplerInitGrammar(vocab, toolCallGrammar, "root"))
	llama.SamplerChainAdd(sampler, llama.SamplerInitTopK(40))
	llama.SamplerChainAdd(sampler, llama.SamplerInitTopP(0.95, 1))
	llama.SamplerChainAdd(sampler, llama.SamplerInitTempExt(0.1, 0.0, 1.0)) // basse temp pour JSON
	llama.SamplerChainAdd(sampler, llama.SamplerInitDist(llama.DefaultSeed))

	// ---- 6. Génération ----
	var sb strings.Builder
	batch := llama.BatchGetOne(tokens)
	for range 256 {
		if _, err := llama.Decode(ctx, batch); err != nil {
			log.Fatalf("Decode : %v", err)
		}
		token := llama.SamplerSample(sampler, ctx, -1)
		if llama.VocabIsEOG(vocab, token) {
			break
		}
		piece := make([]byte, 64)
		if n := llama.TokenToPiece(vocab, token, piece, 0, true); n > 0 {
			sb.Write(piece[:n])
		}
		llama.SamplerAccept(sampler, token)
		batch = llama.BatchGetOne([]llama.Token{token})
	}

	rawOutput := strings.TrimSpace(sb.String())
	fmt.Printf("Sortie brute du modèle :\n%s\n\n", rawOutput)

	// ---- 7. Parser l'appel d'outil ----
	var call ToolCall
	if err := json.Unmarshal([]byte(rawOutput), &call); err != nil {
		log.Fatalf("Impossible de parser l'appel d'outil : %v\nSortie : %s", err, rawOutput)
	}

	fmt.Printf("Outil appelé : %s\n", call.Tool)
	fmt.Printf("Arguments    : %v\n\n", call.Args)

	// ---- 8. Exécuter la fonction Go correspondante ----
	result := dispatchTool(call)
	fmt.Printf("Résultat de l'outil : %s\n", result)
}

// dispatchTool route l'appel vers la bonne fonction Go.
func dispatchTool(call ToolCall) string {
	switch call.Tool {
	case "get_weather":
		city := call.Args["city"]
		return getWeather(city)
	default:
		return fmt.Sprintf("Outil inconnu : %s", call.Tool)
	}
}

// getWeather simule un appel API météo.
func getWeather(city string) string {
	// Dans un vrai programme : appeler une API REST ici.
	return fmt.Sprintf("À %s : 18°C, partiellement nuageux, vent 15 km/h.", city)
}
```

---

## Exécuter

```bash
yzma model get -u https://huggingface.co/Qwen/Qwen3-1.7B-GGUF/resolve/main/Qwen3-1.7B-Q4_K_M.gguf

YZMA_LIB=/chemin/vers/lib go run ./main.go
```

Sortie attendue :

```
Sortie brute du modèle :
{"tool": "get_weather", "args": {"city": "Lyon"}}

Outil appelé : get_weather
Arguments    : map[city:Lyon]

Résultat de l'outil : À Lyon : 18°C, partiellement nuageux, vent 15 km/h.
```

---

## Approche avancée : `SamplerInitGrammarLazyPatterns`

Si le modèle génère parfois du texte avant le `{`, utiliser le sampler _lazy_ qui
n'active la grammaire qu'à partir du premier `{` :

```go
grammarSampler := llama.SamplerInitGrammarLazyPatterns(
    vocab,
    toolCallGrammar,
    "root",
    []string{"{"},  // activer la grammaire dès qu'on voit "{"
    nil,
)
llama.SamplerChainAdd(sampler, grammarSampler)
```

---

## Points importants

| Point | Explication |
|---|---|
| Pas de support natif | yzma ne parse pas les outils — c'est du Go classique |
| Grammaire GBNF | Recommandée pour garantir un JSON valide ; optionnelle mais utile |
| Température basse | `0.1` ou moins pour des sorties JSON déterministes |
| `/no_think` (Qwen3) | Désactive le bloc `<think>` et évite du texte avant le JSON |
| Modèle adapté | Qwen3, Llama-3 Instruct — les modèles de base ne suivent pas les instructions |
