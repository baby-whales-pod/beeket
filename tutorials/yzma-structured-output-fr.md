# Sorties structurées avec yzma

Les **sorties structurées** permettent d'obtenir du modèle une réponse dans un format
précis — JSON, XML, CSV — plutôt que du texte libre. Elles sont essentielles pour
intégrer un LLM dans un pipeline applicatif qui attend des données typées.

Ce tutoriel présente deux approches, de la plus simple à la plus robuste.

---

## Prérequis

| Requis | Version / Note |
|---|---|
| Go | 1.25+ |
| yzma | v1.13.0 |
| Bibliothèques llama.cpp | Installées via `yzma install` |
| Modèle | Qwen3-1.7B-Q4_K_M (ou tout instruct de 1 B+) |

---

## Vue d'ensemble des deux approches

| | Approche 1 : Prompt seul | Approche 2 : Grammaire GBNF |
|---|---|---|
| Complexité | Faible | Moyenne |
| Garantie de JSON valide | Non (validation Go nécessaire) | Oui (contrainte dès le 1er token) |
| Risque de crash | Aucun | SIGABRT si le modèle génère du texte avant `{` |
| Recommandée pour | Prototypage, petits projets | Production, pipelines critiques |

---

## Approche 1 — Prompt seul (recommandée pour commencer)

### Principe

On injecte dans le message système une instruction forte demandant une réponse JSON,
et on ajoute `/no_think` à la fin du message utilisateur (pour les modèles Qwen3 qui
ont un bloc `<think>` par défaut). On valide ensuite la sortie en Go avec
`encoding/json`.

### Exemple complet

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

// Film représente la structure JSON attendue en sortie.
type Film struct {
	Titre    string   `json:"titre"`
	Annee    int      `json:"annee"`
	Genre    []string `json:"genre"`
	Synopsis string   `json:"synopsis"`
}

const modelFile = "Qwen3-1.7B-Q4_K_M.gguf"

// Message système : instruction JSON stricte.
const systemPrompt = `Tu es un assistant spécialisé en cinéma.
Réponds UNIQUEMENT avec un objet JSON valide correspondant au schéma suivant :
{
  "titre":    string,
  "annee":    number,
  "genre":    [string, ...],
  "synopsis": string (max 2 phrases)
}
N'ajoute aucun texte, commentaire ou balise markdown avant ou après le JSON.`

func main() {
	userQuestion := "Donne-moi les informations sur le film Inception. /no_think"

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

	// ---- 3. Prompt ----
	messages := []llama.ChatMessage{
		llama.NewChatMessage("system", systemPrompt),
		llama.NewChatMessage("user", userQuestion),
	}

	tmplBuf := make([]byte, 8192)
	n := llama.ChatApplyTemplate("", messages, true, tmplBuf)
	if n <= 0 {
		log.Fatal("ChatApplyTemplate a échoué")
	}

	// ---- 4. Tokenisation ----
	// addSpecial=true (BOS), parseSpecial=true (reconnaître <|im_end|> etc.)
	tokens := llama.Tokenize(vocab, string(tmplBuf[:n]), true, true)

	// ---- 5. Sampler — température basse pour sortie déterministe ----
	sampler := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
	defer llama.SamplerFree(sampler)
	llama.SamplerChainAdd(sampler, llama.SamplerInitTopK(20))
	llama.SamplerChainAdd(sampler, llama.SamplerInitTopP(0.95, 1))
	llama.SamplerChainAdd(sampler, llama.SamplerInitTempExt(0.1, 0.0, 1.0))
	llama.SamplerChainAdd(sampler, llama.SamplerInitDist(llama.DefaultSeed))

	// Stop strings : arrêter sur <|im_end|> (marqueur de fin Qwen/ChatML)
	stopTokens := []string{"<|im_end|>", "<|endoftext|>"}

	// ---- 6. Génération ----
	var sb strings.Builder
	batch := llama.BatchGetOne(tokens)

loop:
	for range 512 {
		if _, err := llama.Decode(ctx, batch); err != nil {
			log.Fatalf("Decode : %v", err)
		}
		token := llama.SamplerSample(sampler, ctx, -1)
		if llama.VocabIsEOG(vocab, token) {
			break
		}
		piece := make([]byte, 64)
		if n := llama.TokenToPiece(vocab, token, piece, 0, true); n > 0 {
			fragment := string(piece[:n])
			sb.WriteString(fragment)
			// Vérifier les stop strings dans la sortie accumulée
			current := sb.String()
			for _, stop := range stopTokens {
				if strings.Contains(current, stop) {
					break loop
				}
			}
		}
		llama.SamplerAccept(sampler, token)
		batch = llama.BatchGetOne([]llama.Token{token})
	}

	// ---- 7. Extraire et valider le JSON ----
	raw := strings.TrimSpace(sb.String())
	// Supprimer d'éventuels blocs markdown ```json ... ```
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	// Extraire le premier objet JSON complet
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end == -1 || end <= start {
		log.Fatalf("Aucun JSON trouvé dans la sortie :\n%s", raw)
	}
	jsonStr := raw[start : end+1]

	var film Film
	if err := json.Unmarshal([]byte(jsonStr), &film); err != nil {
		log.Fatalf("JSON invalide : %v\nSortie brute :\n%s", err, jsonStr)
	}

	// ---- 8. Afficher le résultat typé ----
	fmt.Printf("Titre    : %s\n", film.Titre)
	fmt.Printf("Année    : %d\n", film.Annee)
	fmt.Printf("Genre    : %s\n", strings.Join(film.Genre, ", "))
	fmt.Printf("Synopsis : %s\n", film.Synopsis)
}
```

Sortie attendue :

```
Titre    : Inception
Année    : 2010
Genre    : Science-fiction, Thriller, Action
Synopsis : Dom Cobb est un voleur spécialisé dans l'extraction de secrets depuis
           les rêves de ses cibles. Il reçoit la mission inverse : implanter une idée
           dans l'esprit d'un héritier d'empire industriel.
```

---

## Approche 2 — Grammaire GBNF (avancée)

### Principe

On fournit un fichier de grammaire GBNF à `SamplerInitGrammar`. llama.cpp masque
tous les tokens incompatibles avec la grammaire avant l'échantillonnage — le modèle
**ne peut pas** générer de JSON invalide.

### Grammaire JSON générique

```go
// jsonGrammar est une grammaire GBNF qui contraint la sortie à du JSON valide.
// Inspirée du fichier json.gbnf de llama.cpp.
const jsonGrammar = `root   ::= object
object ::= "{" ws (pair ("," ws pair)*)? ws "}"
pair   ::= string ws ":" ws value
array  ::= "[" ws (value ("," ws value)*)? ws "]"
value  ::= object | array | string | number | "true" | "false" | "null"
string ::= "\"" ([^"\\] | "\\" .)* "\""
number ::= "-"? ([0-9] | [1-9][0-9]*) ("." [0-9]+)? (([eE] [+-]? [0-9]+))?
ws     ::= [ \t\n]*
`
```

### Ajout dans la chaîne de samplers

```go
vocab := llama.ModelGetVocab(model)

// Ajouter le sampler de grammaire EN PREMIER dans la chaîne
grammarSampler := llama.SamplerInitGrammar(vocab, jsonGrammar, "root")

sampler := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
defer llama.SamplerFree(sampler)
llama.SamplerChainAdd(sampler, grammarSampler) // ← en premier
llama.SamplerChainAdd(sampler, llama.SamplerInitTopK(40))
llama.SamplerChainAdd(sampler, llama.SamplerInitTopP(0.95, 1))
llama.SamplerChainAdd(sampler, llama.SamplerInitTempExt(0.1, 0.0, 1.0))
llama.SamplerChainAdd(sampler, llama.SamplerInitDist(llama.DefaultSeed))
```

> ⚠️ **Risque de SIGABRT** : si le modèle génère du texte avant `{`, la grammaire
> est violée et llama.cpp peut terminer le processus. Utilisez
> `SamplerInitGrammarLazyPatterns` pour n'activer la grammaire qu'à partir du
> premier `{` :

```go
grammarSampler := llama.SamplerInitGrammarLazyPatterns(
    vocab,
    jsonGrammar,
    "root",
    []string{"{"},  // déclencher la grammaire au premier "{"
    nil,
)
```

### Grammaire pour un schéma précis

Pour contraindre la sortie à un schéma spécifique (ex. : `Film`), écrire une grammaire
ciblée plutôt que la grammaire JSON générique :

```go
const filmGrammar = `
root     ::= "{" ws film ws "}"
film     ::= "\"titre\"" ws ":" ws string ws ","
             ws "\"annee\""  ws ":" ws number ws ","
             ws "\"genre\""  ws ":" ws genres ws ","
             ws "\"synopsis\"" ws ":" ws string
genres   ::= "[" ws string (ws "," ws string)* ws "]"
string   ::= "\"" ([^"\\] | "\\" .)* "\""
number   ::= [0-9]+
ws       ::= [ \t\n\r]*
`
```

---

## Exécuter (approche 1)

```bash
yzma model get -u https://huggingface.co/Qwen/Qwen3-1.7B-GGUF/resolve/main/Qwen3-1.7B-Q4_K_M.gguf

YZMA_LIB=/chemin/vers/lib go run ./main.go
```

---

## Récapitulatif des bonnes pratiques

| Pratique | Raison |
|---|---|
| Température basse (≤ 0.1) | Réduit la variabilité, le modèle suit mieux l'instruction JSON |
| `/no_think` (Qwen3) | Supprime le bloc `<think>` qui précède la réponse et pollue le JSON |
| Extraire `{...}` avant `Unmarshal` | Robustesse face aux espaces ou retours à la ligne résiduels |
| Valider avec `json.Unmarshal` | Détecter les champs manquants ou les types incorrects |
| Grammaire GBNF | Garantie formelle — utiliser en production si la robustesse est critique |

---

## Pour aller plus loin

- Combiner avec le [message système](./yzma-exemple-message-systeme-fr.md) pour
  affiner le comportement du modèle.
- Utiliser les [embeddings](./yzma-embeddings-fr.md) pour un pipeline RAG qui injecte
  du contexte avant de demander une sortie JSON.
- Consulter `grammars/` dans le dépôt llama.cpp pour des grammaires GBNF prêtes à
  l'emploi (JSON, Markdown, listes, etc.).
