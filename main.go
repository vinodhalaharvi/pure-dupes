// main.go
package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed index.html
var content embed.FS

// ============================================================================
// DEFERRED COMPUTATION
// ============================================================================

type Thunk[A any] struct {
	Compute func() A
	Name    string
}

func RunThunk[A any](t Thunk[A]) A {
	return t.Compute()
}

func ParallelRunThunks[A any](thunks []Thunk[A], numWorkers int) []A {
	if numWorkers <= 0 {
		numWorkers = 1
	}

	results := make([]A, len(thunks))
	jobs := make(chan int, len(thunks))
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				results[idx] = RunThunk(thunks[idx])
			}
		}()
	}

	for i := range thunks {
		jobs <- i
	}
	close(jobs)

	wg.Wait()
	return results
}

// ============================================================================
// DOMAIN TYPES
// ============================================================================

type MerkleNode struct {
	Hash     []byte
	Children []MerkleNode
	IsLeaf   bool
}

type FileTree struct {
	Path       string
	Root       []byte
	Tree       MerkleNode
	Size       int64
	ChunkCount int
	Leaves     []string
	Depth      int
}

type DuplicateMatch struct {
	TargetPath string
	Similarity float64
	SharedSize int64
}

type FileNode struct {
	Path         string
	Name         string
	IsDir        bool
	Children     []FileNode
	Matches      []DuplicateMatch
	BestMatch    float64
	Size         int64
	RelativePath string
}

type DuplicateGroup struct {
	Files      []string
	Similarity float64
	Size       int64
}

type DedupResult struct {
	RootTree        FileNode
	AllMatches      map[string][]DuplicateMatch
	DuplicateGroups []DuplicateGroup
	TotalFiles      int
	UniqueFiles     int
	FullDupCount    int
	PartialDupCount int
	SpaceSaved      int64
}

// ============================================================================
// MONOID
// ============================================================================

type Monoid[A any] struct {
	Empty   func() A
	Combine func(A, A) A
}

var SHA256Monoid = Monoid[[]byte]{
	Empty: func() []byte { return []byte{} },
	Combine: func(a, b []byte) []byte {
		h := sha256.New()
		h.Write(a)
		h.Write(b)
		return h.Sum(nil)
	},
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

func Map[A, B any](xs []A, f func(A) B) []B {
	result := make([]B, len(xs))
	for i, x := range xs {
		result[i] = f(x)
	}
	return result
}

func Filter[A any](xs []A, pred func(A) bool) []A {
	result := []A{}
	for _, x := range xs {
		if pred(x) {
			result = append(result, x)
		}
	}
	return result
}

func GroupBy[A any, K comparable](xs []A, key func(A) K) map[K][]A {
	groups := make(map[K][]A)
	for _, x := range xs {
		k := key(x)
		groups[k] = append(groups[k], x)
	}
	return groups
}

// ============================================================================
// MERKLE TREE
// ============================================================================

func HashLeaf(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

func BuildMerkleTree(hashes [][]byte, m Monoid[[]byte]) MerkleNode {
	if len(hashes) == 0 {
		return MerkleNode{Hash: m.Empty(), IsLeaf: true, Children: []MerkleNode{}}
	}
	if len(hashes) == 1 {
		return MerkleNode{Hash: hashes[0], IsLeaf: true, Children: []MerkleNode{}}
	}

	nextLevel := []MerkleNode{}

	for i := 0; i < len(hashes); i += 2 {
		if i+1 < len(hashes) {
			left := MerkleNode{Hash: hashes[i], IsLeaf: true, Children: []MerkleNode{}}
			right := MerkleNode{Hash: hashes[i+1], IsLeaf: true, Children: []MerkleNode{}}
			combined := m.Combine(hashes[i], hashes[i+1])

			parent := MerkleNode{
				Hash:     combined,
				IsLeaf:   false,
				Children: []MerkleNode{left, right},
			}
			nextLevel = append(nextLevel, parent)
		} else {
			single := MerkleNode{Hash: hashes[i], IsLeaf: true, Children: []MerkleNode{}}
			nextLevel = append(nextLevel, single)
		}
	}

	if len(nextLevel) == 1 {
		return nextLevel[0]
	}

	parentHashes := Map(nextLevel, func(n MerkleNode) []byte { return n.Hash })
	upperTree := BuildMerkleTree(parentHashes, m)
	upperTree.Children = nextLevel
	return upperTree
}

func collectLeaves(node MerkleNode) [][]byte {
	if node.IsLeaf {
		return [][]byte{node.Hash}
	}

	leaves := [][]byte{}
	for _, child := range node.Children {
		childLeaves := collectLeaves(child)
		leaves = append(leaves, childLeaves...)
	}
	return leaves
}

// ============================================================================
// FILE PROCESSING
// ============================================================================

func calculateDepth(rootDir, filePath string) int {
	rel, err := filepath.Rel(rootDir, filePath)
	if err != nil {
		return 0
	}
	if rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

func ProcessFileThunk(path string, chunkSize int, rootDir string) Thunk[FileTree] {
	return Thunk[FileTree]{
		Compute: func() FileTree {
			file, err := os.Open(path)
			if err != nil {
				return FileTree{}
			}
			defer file.Close()

			stat, err := file.Stat()
			if err != nil {
				return FileTree{}
			}

			chunks := [][]byte{}
			buffer := make([]byte, chunkSize)

			for {
				n, err := file.Read(buffer)
				if n > 0 {
					chunk := make([]byte, n)
					copy(chunk, buffer[:n])
					chunks = append(chunks, chunk)
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					return FileTree{}
				}
			}

			hashes := Map(chunks, HashLeaf)
			tree := BuildMerkleTree(hashes, SHA256Monoid)
			root := tree.Hash

			leafBytes := collectLeaves(tree)
			leaves := Map(leafBytes, func(b []byte) string {
				return hex.EncodeToString(b)
			})

			depth := calculateDepth(rootDir, path)

			return FileTree{
				Path:       path,
				Root:       root,
				Tree:       tree,
				Size:       stat.Size(),
				ChunkCount: len(chunks),
				Leaves:     leaves,
				Depth:      depth,
			}
		},
		Name: fmt.Sprintf("ProcessFile(%s)", filepath.Base(path)),
	}
}

func ScanDirectoryThunk(dir string, maxDepth int) Thunk[[]string] {
	return Thunk[[]string]{
		Compute: func() []string {
			var files []string

			filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}

				depth := calculateDepth(dir, path)

				if info.IsDir() && depth > maxDepth {
					return filepath.SkipDir
				}

				if !info.IsDir() && depth <= maxDepth {
					files = append(files, path)
				}

				return nil
			})

			return files
		},
		Name: fmt.Sprintf("ScanDir(%s, maxDepth=%d)", filepath.Base(dir), maxDepth),
	}
}

// ============================================================================
// OPTIMIZED DEDUPLICATION
// ============================================================================

func BuildChunkIndex(files []FileTree) map[string][]int {
	index := make(map[string][]int)
	for i, file := range files {
		for _, chunkHash := range file.Leaves {
			index[chunkHash] = append(index[chunkHash], i)
		}
	}
	return index
}

func FindCandidates(sourceFile FileTree, chunkIndex map[string][]int, threshold float64) map[int]int {
	candidates := make(map[int]int)

	for _, chunkHash := range sourceFile.Leaves {
		if targets, exists := chunkIndex[chunkHash]; exists {
			for _, targetIdx := range targets {
				candidates[targetIdx]++
			}
		}
	}

	minSharedChunks := int(float64(len(sourceFile.Leaves)) * threshold)
	filtered := make(map[int]int)
	for idx, count := range candidates {
		if count >= minSharedChunks {
			filtered[idx] = count
		}
	}

	return filtered
}

func CompareFiles(a, b FileTree) float64 {
	if hex.EncodeToString(a.Root) == hex.EncodeToString(b.Root) {
		return 1.0
	}

	if len(a.Leaves) == 0 || len(b.Leaves) == 0 {
		return 0.0
	}

	setB := make(map[string]bool, len(b.Leaves))
	for _, leaf := range b.Leaves {
		setB[leaf] = true
	}

	matches := 0
	for _, leaf := range a.Leaves {
		if setB[leaf] {
			matches++
		}
	}

	return float64(matches) / float64(len(a.Leaves))
}

// ============================================================================
// TREE BUILDING
// ============================================================================

func BuildFileTree(rootPath string, files []FileTree, matches map[string][]DuplicateMatch, maxDepth int) FileNode {
	relFiles := make(map[string]FileTree)
	for _, f := range files {
		rel, _ := filepath.Rel(rootPath, f.Path)
		relFiles[rel] = f
	}

	root := FileNode{
		Path:         rootPath,
		Name:         filepath.Base(rootPath),
		IsDir:        true,
		Children:     []FileNode{},
		RelativePath: "",
	}

	for rel, ft := range relFiles {
		parts := strings.Split(rel, string(filepath.Separator))
		addToTree(&root, parts, ft, matches, 1, maxDepth, rootPath)
	}

	return root
}

func addToTree(node *FileNode, parts []string, ft FileTree, matches map[string][]DuplicateMatch, depth int, maxDepth int, rootPath string) {
	if depth > maxDepth {
		return
	}

	if len(parts) == 0 {
		return
	}

	if len(parts) == 1 {
		fileMatches := matches[ft.Path]
		bestMatch := 0.0
		if len(fileMatches) > 0 {
			for _, m := range fileMatches {
				if m.Similarity > bestMatch {
					bestMatch = m.Similarity
				}
			}
		}

		node.Children = append(node.Children, FileNode{
			Path:         ft.Path,
			Name:         parts[0],
			IsDir:        false,
			Children:     []FileNode{},
			Matches:      fileMatches,
			BestMatch:    bestMatch,
			Size:         ft.Size,
			RelativePath: ft.Path,
		})
		return
	}

	dirName := parts[0]
	var dirNode *FileNode
	for i := range node.Children {
		if node.Children[i].Name == dirName && node.Children[i].IsDir {
			dirNode = &node.Children[i]
			break
		}
	}

	if dirNode == nil {
		currentPath := filepath.Join(node.Path, dirName)
		relativePath, _ := filepath.Rel(rootPath, currentPath)

		node.Children = append(node.Children, FileNode{
			Name:         dirName,
			Path:         currentPath,
			RelativePath: relativePath,
			IsDir:        true,
			Children:     []FileNode{},
		})
		dirNode = &node.Children[len(node.Children)-1]
	}

	addToTree(dirNode, parts[1:], ft, matches, depth+1, maxDepth, rootPath)
}

// ============================================================================
// MAIN DEDUPLICATION LOGIC
// ============================================================================

func FindDuplicatesThunk(dir string, threshold float64, maxDepth int, numWorkers int, chunkSize int) Thunk[DedupResult] {
	dirScan := ScanDirectoryThunk(dir, maxDepth)

	return Thunk[DedupResult]{
		Compute: func() DedupResult {
			// Step 1: Scan directory
			allFiles := RunThunk(dirScan)

			// Step 2: Build thunks for all files
			fileThunks := Map(allFiles, func(path string) Thunk[FileTree] {
				return ProcessFileThunk(path, chunkSize, dir)
			})

			// Step 3: Execute in parallel
			fileTrees := Filter(ParallelRunThunks(fileThunks, numWorkers), func(ft FileTree) bool {
				return ft.Path != ""
			})

			// Step 4: Group by merkle root (exact duplicates)
			filesByRoot := GroupBy(fileTrees, func(ft FileTree) string {
				return hex.EncodeToString(ft.Root)
			})

			allMatches := make(map[string][]DuplicateMatch)
			duplicateGroups := []DuplicateGroup{}
			fullDupCount := 0
			partialDupCount := 0
			spaceSaved := int64(0)

			processedFiles := make(map[string]bool)

			// Process exact duplicates
			for _, group := range filesByRoot {
				if len(group) > 1 {
					// Found exact duplicates
					groupFiles := Map(group, func(ft FileTree) string { return ft.Path })

					duplicateGroups = append(duplicateGroups, DuplicateGroup{
						Files:      groupFiles,
						Similarity: 1.0,
						Size:       group[0].Size,
					})

					// Create matches for each file
					for i, src := range group {
						matches := []DuplicateMatch{}
						for j, tgt := range group {
							if i != j {
								matches = append(matches, DuplicateMatch{
									TargetPath: tgt.Path,
									Similarity: 1.0,
									SharedSize: src.Size,
								})
							}
						}
						if len(matches) > 0 {
							allMatches[src.Path] = matches
							fullDupCount++
							if !processedFiles[src.Path] {
								spaceSaved += src.Size
								processedFiles[src.Path] = true
							}
						}
					}
				}
			}

			// Step 5: Find partial duplicates
			chunkIndex := BuildChunkIndex(fileTrees)

			for i := 0; i < len(fileTrees); i++ {
				src := fileTrees[i]

				// Skip if already has exact match
				if _, hasExact := allMatches[src.Path]; hasExact {
					continue
				}

				candidates := FindCandidates(src, chunkIndex, threshold)
				matches := []DuplicateMatch{}

				for targetIdx := range candidates {
					if targetIdx == i {
						continue
					}

					tgt := fileTrees[targetIdx]

					// Skip if target already has exact match with source
					srcRoot := hex.EncodeToString(src.Root)
					tgtRoot := hex.EncodeToString(tgt.Root)
					if srcRoot == tgtRoot {
						continue
					}

					similarity := CompareFiles(src, tgt)

					if similarity >= threshold && similarity < 1.0 {
						matches = append(matches, DuplicateMatch{
							TargetPath: tgt.Path,
							Similarity: similarity,
							SharedSize: int64(float64(src.Size) * similarity),
						})
					}
				}

				if len(matches) > 0 {
					allMatches[src.Path] = matches
					partialDupCount++
				}
			}

			// Step 6: Build UI tree
			tree := BuildFileTree(dir, fileTrees, allMatches, maxDepth)

			uniqueCount := len(fileTrees) - fullDupCount - partialDupCount

			return DedupResult{
				RootTree:        tree,
				AllMatches:      allMatches,
				DuplicateGroups: duplicateGroups,
				TotalFiles:      len(fileTrees),
				UniqueFiles:     uniqueCount,
				FullDupCount:    fullDupCount,
				PartialDupCount: partialDupCount,
				SpaceSaved:      spaceSaved,
			}
		},
		Name: fmt.Sprintf("FindDuplicates(dir:%s, thresh:%.0f%%, depth:%d, workers:%d, chunk:%d)",
			filepath.Base(dir), threshold*100, maxDepth, numWorkers, chunkSize),
	}
}

// ============================================================================
// HTTP SERVER
// ============================================================================

func handleDedup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Dir        string  `json:"dir"`
		Threshold  float64 `json:"threshold"`
		MaxDepth   int     `json:"maxDepth"`
		NumWorkers int     `json:"numWorkers"`
		ChunkSize  int     `json:"chunkSize"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.NumWorkers <= 0 {
		req.NumWorkers = runtime.NumCPU()
	}

	if req.ChunkSize <= 0 {
		req.ChunkSize = 4096
	}

	dedupThunk := FindDuplicatesThunk(req.Dir, req.Threshold, req.MaxDepth, req.NumWorkers, req.ChunkSize)

	result := RunThunk(dedupThunk)
	json.NewEncoder(w).Encode(result)
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	}
	if err != nil {
		fmt.Println("Please open your browser to:", url)
	}
}

func findAvailablePort(startPort int) (int, error) {
	for port := startPort; port < startPort+100; port++ {
		addr := fmt.Sprintf(":%d", port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			listener.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports found")
}

func waitForServer(url string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// ============================================================================
// MAIN
// ============================================================================

func main() {
	port, err := findAvailablePort(8080)
	if err != nil {
		fmt.Println("Error finding available port:", err)
		return
	}

	fsys, err := fs.Sub(content, ".")
	if err != nil {
		panic(err)
	}
	http.Handle("/", http.FileServer(http.FS(fsys)))
	http.HandleFunc("/api/dedup", handleDedup)

	url := fmt.Sprintf("http://localhost:%d", port)

	fmt.Println("🔍 pure-dupes - Find duplicate files")
	fmt.Println("📊 Server:", url)
	fmt.Println("🌳 Merkle tree-based deduplication")
	fmt.Printf("💻 Default workers: %d (CPU cores)\n", runtime.NumCPU())
	fmt.Println()

	serverReady := make(chan bool)
	go func() {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			fmt.Println("Error starting server:", err)
			serverReady <- false
			return
		}
		serverReady <- true
		http.Serve(listener, nil)
	}()

	if <-serverReady {
		if waitForServer(url, 5*time.Second) {
			openBrowser(url)
		} else {
			fmt.Println("Server started but not responding. Please open your browser to:", url)
		}
	}

	select {}
}
