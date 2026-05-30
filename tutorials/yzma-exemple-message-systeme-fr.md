# Ajouter un message système à un chat yzma

Ce tutoriel complète l'[exemple 16 du tutoriel principal](./yzma-tutorial-fr.md) en
montrant comment inclure un **message système** dans la conversation. Le message
système permet de donner au modèle une personnalité, un rôle ou des contraintes
précises avant que l'utilisateur ne pose sa question.

---

## Prérequis

| Requis | Version |
|---|---|
| Go | 1.25+ |
| yzma | v1.13.0 |
| Bibliothèques llama.cpp | Installées via `yzma install` |
| Modèle GGUF | Téléchargé dans `~/models/` |

> Ce tutoriel utilise `Qwen3-1.7B-Q4_K_M.gguf`. Vous pouvez le remplacer par
> n'importe quel modèle instruct compatible ChatML (Llama-3, Mistral, etc.).

---

## Le rôle `"system"` dans les messages de chat

La plupart des modèles instruct reconnaissent trois rôles dans un échange :

| Rôle | Description |
|---|---|
| `"system"` | Instruction de haut niveau donnée **avant** la conversation |
| `"user"` | Message de l'utilisateur humain |
| `"assistant"` | Réponse précédente du modèle (pour les échanges multi-tours) |

Avec yzma, on crée chaque message via `llama.NewChatMessage(role, content)` et on
les passe dans l'ordre à `llama.ChatApplyTemplate`. Le template du modèle se charge
de les formater correctement (balises ChatML `<|im_start|>`, marqueurs Llama-3, etc.).

---

## Exemple complet

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
	modelName := flag.String("model", "Qwen3-1.7B-Q4_K_M.gguf",
		"Nom du fichier GGUF dans ~/models/")
	systemMsg := flag.String("system", "Tu es un chef cuisinier français expert. "+
		"Réponds toujours de façon concise et pratique.",
		"Message système")
	userMsg := flag.String("prompt", "Comment réussir une béchamel sans grumeaux ?",
		"Message utilisateur")
	maxTokens := flag.Int("max", 300, "Nombre maximal de tokens générés")
	flag.Parse()

	// ---- 1. Charger la bibliothèque partagée ----
	libPath := os.Getenv("YZMA_LIB")
	if err := llama.Load(libPath); err != nil {
		log.Fatalf("llama.Load : %v", err)
	}
	llama.LogSet(llama.LogSilent())
	llama.Init()
	defer llama.Close()

	// ---- 2. Charger le modèle ----
	modelPath := filepath.Join(download.DefaultModelsDir(), *modelName)
	modelParams := llama.ModelDefaultParams()
	// Pour GPU : modelParams.NGpuLayers = 99

	model, err := llama.ModelLoadFromFile(modelPath, modelParams)
	if err != nil {
		log.Fatalf("ModelLoadFromFile : %v", err)
	}
	defer func() { _ = llama.ModelFree(model) }()

	// ---- 3. Créer le contexte ----
	ctxParams := llama.ContextDefaultParams()
	ctx, err := llama.InitFromModel(model, ctxParams)
	if err != nil {
		log.Fatalf("InitFromModel : %v", err)
	}
	defer func() { _ = llama.Free(ctx) }()

	// ---- 4. Construire le prompt avec message système ----
	vocab := llama.ModelGetVocab(model)

	// L'ordre est important : system → user (→ assistant pour les tours suivants)
	messages := []llama.ChatMessage{
		llama.NewChatMessage("system", *systemMsg),
		llama.NewChatMessage("user", *userMsg),
	}

	tmplBuf := make([]byte, 8192)
	n := llama.ChatApplyTemplate("", messages, true, tmplBuf)
	if n <= 0 {
		log.Fatal("ChatApplyTemplate a échoué")
	}
	formattedPrompt := string(tmplBuf[:n])

	// ---- 5. Tokeniser ----
	tokens := llama.Tokenize(vocab, formattedPrompt, true, true)
	if len(tokens) == 0 {
		log.Fatal("tokenisation vide")
	}

	// ---- 6. Sampler ----
	sampler := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
	defer llama.SamplerFree(sampler)
	llama.SamplerChainAdd(sampler, llama.SamplerInitTopK(40))
	llama.SamplerChainAdd(sampler, llama.SamplerInitTopP(0.95, 1))
	llama.SamplerChainAdd(sampler, llama.SamplerInitTempExt(0.8, 0.0, 1.0))
	llama.SamplerChainAdd(sampler, llama.SamplerInitDist(llama.DefaultSeed))

	// ---- 7. Génération ----
	fmt.Printf("\nSystème  : %s\n", *systemMsg)
	fmt.Printf("Utilisateur : %s\n\nAssistant : ", *userMsg)

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
		if n := llama.TokenToPiece(vocab, token, piece, 0, true); n > 0 {
			fmt.Print(string(piece[:n]))
		}
		llama.SamplerAccept(sampler, token)
		batch = llama.BatchGetOne([]llama.Token{token})
	}
	fmt.Println()
}
```

---

## Exécuter

```bash
# Télécharger le modèle (une fois)
yzma model get -u https://huggingface.co/Qwen/Qwen3-1.7B-GGUF/resolve/main/Qwen3-1.7B-Q4_K_M.gguf

# Lancer avec les valeurs par défaut
YZMA_LIB=/chemin/vers/lib go run ./main.go

# Changer le rôle système et la question
YZMA_LIB=/chemin/vers/lib go run ./main.go \
  -system "Tu es un professeur de mathématiques patient et pédagogue." \
  -prompt "Explique-moi le théorème de Pythagore."
```

Sortie attendue :

```
Système  : Tu es un chef cuisinier français expert. Réponds toujours de façon concise et pratique.
Utilisateur : Comment réussir une béchamel sans grumeaux ?

Assistant : Faites fondre le beurre à feu doux, ajoutez la farine en une seule fois
et fouettez énergiquement hors du feu pendant 1 minute pour former un roux blanc.
Incorporez ensuite le lait chaud (pas froid !) petit à petit en fouettant sans arrêt.
Remettez sur feu moyen et remuez jusqu'à épaississement. Le secret : lait chaud +
fouet constant + roux bien cuit.
```

---

## Ce qu'il faut retenir

- **Toujours placer `"system"` avant `"user"`** dans la slice de messages.
- **`addAssistantPrompt = true`** dans `ChatApplyTemplate` ajoute le marqueur
  d'ouverture de réponse du modèle — indispensable pour la génération.
- Si vous n'avez pas besoin de message système, supprimez simplement l'entrée
  `"system"` ; le comportement par défaut du modèle s'applique.
- Pour un échange multi-tours, ajoutez les messages `"assistant"` précédents dans la
  slice avant le nouveau message `"user"`.
