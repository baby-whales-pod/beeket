# Embeddings avec yzma

Les **embeddings** sont des vecteurs de nombres réels qui représentent le sens d'un
texte dans un espace mathématique. Deux textes sémantiquement proches auront des
vecteurs proches (cosinus élevé). Ils servent à la recherche sémantique, au clustering,
à la détection de doublons, et à la base des pipelines RAG (_Retrieval-Augmented
Generation_).

---

## Prérequis

| Requis | Version |
|---|---|
| Go | 1.25+ |
| yzma | v1.13.0 |
| Bibliothèques llama.cpp | Installées via `yzma install` |
| Modèle d'embedding GGUF | Téléchargé dans `~/models/` |

> Ce tutoriel utilise `nomic-embed-text-v1.5.Q4_K_M.gguf`. Tout modèle d'embedding
> au format GGUF convient (bge-small-en, mxbai-embed-large, etc.).

---

## Différence avec l'inférence de texte

| Aspect | Modèle de génération | Modèle d'embedding |
|---|---|---|
| Objectif | Générer des tokens | Encoder un texte en vecteur |
| API yzma | `llama.Decode` + sampler | `llama.Encode` + `GetEmbeddingsSeq` |
| Paramètre clé | aucun | `ctxParams.Embeddings = 1` |
| Sortie | Tokens texte | `[]float32` de dimension `nEmbd` |

---

## Étapes clés

### 1. Activer le mode embeddings dans `ContextParams`

```go
ctxParams := llama.ContextDefaultParams()
ctxParams.Embeddings = 1                          // activer la sortie embeddings
ctxParams.PoolingType = llama.PoolingTypeUnspecified // laisser le modèle décider
```

`PoolingTypeUnspecified` (-1) demande à llama.cpp d'utiliser la stratégie de pooling
encodée dans le modèle GGUF lui-même. Les modèles nomic et bge utilisent en général
`Mean` ou `CLS` ; laisser le modèle choisir est la valeur sûre.

### 2. Appeler `llama.Encode` (et non `Decode`)

```go
batch := llama.BatchGetOne(tokens)
if _, err := llama.Encode(ctx, batch); err != nil {
    log.Fatalf("Encode : %v", err)
}
```

### 3. Récupérer le vecteur

```go
nEmbd := llama.ModelNEmbd(model)
vec, err := llama.GetEmbeddingsSeq(ctx, 0, nEmbd)
if err != nil || vec == nil {
    log.Fatal("GetEmbeddingsSeq a échoué")
}
```

### 4. Normaliser en L2

La plupart des utilisations (cosinus, dot-product) supposent des vecteurs normalisés :

```go
func normalizeL2(v []float32) []float32 {
    var sum float64
    for _, x := range v {
        sum += float64(x) * float64(x)
    }
    norm := float32(1.0 / math.Sqrt(sum))
    out := make([]float32, len(v))
    for i, x := range v {
        out[i] = x * norm
    }
    return out
}
```

---

## Exemple complet : similarité cosinus entre deux phrases

```go
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"

	"github.com/hybridgroup/yzma/pkg/download"
	"github.com/hybridgroup/yzma/pkg/llama"
)

const modelFile = "nomic-embed-text-v1.5.Q4_K_M.gguf"

func main() {
	// ---- 1. Charger la bibliothèque ----
	libPath := os.Getenv("YZMA_LIB")
	if err := llama.Load(libPath); err != nil {
		log.Fatalf("llama.Load : %v", err)
	}
	llama.LogSet(llama.LogSilent())
	llama.Init()
	defer llama.Close()

	// ---- 2. Charger le modèle ----
	modelPath := filepath.Join(download.DefaultModelsDir(), modelFile)
	model, err := llama.ModelLoadFromFile(modelPath, llama.ModelDefaultParams())
	if err != nil {
		log.Fatalf("ModelLoadFromFile : %v", err)
	}
	defer func() { _ = llama.ModelFree(model) }()

	// ---- 3. Contexte en mode embeddings ----
	ctxParams := llama.ContextDefaultParams()
	ctxParams.Embeddings = 1                             // ← indispensable
	ctxParams.PoolingType = llama.PoolingTypeUnspecified // ← laisser le modèle décider

	ctx, err := llama.InitFromModel(model, ctxParams)
	if err != nil {
		log.Fatalf("InitFromModel : %v", err)
	}
	defer func() { _ = llama.Free(ctx) }()

	vocab := llama.ModelGetVocab(model)
	nEmbd := llama.ModelNEmbd(model)

	// ---- 4. Encoder deux phrases ----
	phrases := []string{
		"Le chat dort sur le canapé.",
		"Un félin somnole sur le sofa.",
		"La fusée SpaceX a atterri hier soir.",
	}

	embeddings := make([][]float32, len(phrases))
	for i, phrase := range phrases {
		emb, err := encode(ctx, vocab, model, phrase, nEmbd)
		if err != nil {
			log.Fatalf("encode(%q) : %v", phrase, err)
		}
		embeddings[i] = emb
		fmt.Printf("Phrase %d encodée : %d dimensions\n", i+1, len(emb))
	}

	// ---- 5. Calculer les similarités cosinus ----
	fmt.Println()
	for i := 0; i < len(phrases); i++ {
		for j := i + 1; j < len(phrases); j++ {
			sim := cosineSim(embeddings[i], embeddings[j])
			fmt.Printf("sim(%d, %d) = %.4f  |  %q ↔ %q\n",
				i+1, j+1, sim, phrases[i], phrases[j])
		}
	}
}

// encode tokenise une phrase, l'encode et retourne le vecteur L2-normalisé.
func encode(ctx llama.Context, vocab llama.Vocab, model llama.Model, text string, nEmbd int32) ([]float32, error) {
	// Tokeniser (addSpecial=true pour BOS/EOS, parseSpecial=true)
	tokens := llama.Tokenize(vocab, text, true, true)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("tokenisation vide pour %q", text)
	}

	// Encoder (modèles d'embedding utilisent Encode, pas Decode)
	batch := llama.BatchGetOne(tokens)
	if _, err := llama.Encode(ctx, batch); err != nil {
		return nil, fmt.Errorf("Encode : %w", err)
	}

	// Récupérer le vecteur de la séquence 0
	vec, err := llama.GetEmbeddingsSeq(ctx, 0, nEmbd)
	if err != nil || vec == nil {
		return nil, fmt.Errorf("GetEmbeddingsSeq : %w", err)
	}

	return normalizeL2(vec), nil
}

// normalizeL2 divise chaque composante par la norme L2 du vecteur.
func normalizeL2(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	norm := float32(1.0 / math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * norm
	}
	return out
}

// cosineSim calcule la similarité cosinus entre deux vecteurs L2-normalisés.
// Pour des vecteurs normalisés, cosinus = produit scalaire.
func cosineSim(a, b []float32) float64 {
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}
```

---

## Exécuter

```bash
# Télécharger le modèle (une fois)
yzma model get -u https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q4_K_M.gguf

# Lancer
YZMA_LIB=/chemin/vers/lib go run ./main.go
```

Sortie attendue :

```
Phrase 1 encodée : 768 dimensions
Phrase 2 encodée : 768 dimensions
Phrase 3 encodée : 768 dimensions

sim(1, 2) = 0.9312  |  "Le chat dort sur le canapé." ↔ "Un félin somnole sur le sofa."
sim(1, 3) = 0.3041  |  "Le chat dort sur le canapé." ↔ "La fusée SpaceX a atterri hier soir."
sim(2, 3) = 0.2987  |  "Un félin somnole sur le sofa." ↔ "La fusée SpaceX a atterri hier soir."
```

Les deux premières phrases (synonymes sémantiques) ont une similarité proche de 0.93,
tandis que les phrases hors-sujet restent sous 0.31.

---

## Points importants

| Point | Explication |
|---|---|
| `ctxParams.Embeddings = 1` | **Obligatoire** — sans ce flag, `GetEmbeddingsSeq` retourne `nil` |
| `llama.Encode` vs `llama.Decode` | Utiliser `Encode` pour les modèles encodeur-seulement |
| `PoolingTypeUnspecified` | Délègue le choix au modèle — recommandé pour débuter |
| Normalisation L2 | Nécessaire pour que le produit scalaire soit équivalent au cosinus |
| Réinitialiser le contexte | Pour encoder plusieurs textes séquentiellement, le contexte KV est réutilisé automatiquement avec `BatchGetOne` |

---

## Pour aller plus loin

- **Recherche sémantique** : stocker les vecteurs dans un index (pgvector, Qdrant, Weaviate) et interroger par cosinus.
- **RAG** : encoder vos documents au préalable, puis retrouver les passages pertinents avant de les injecter dans le prompt d'un modèle génératif.
- **Clustering** : appliquer k-means sur les embeddings pour regrouper des textes par thème.
