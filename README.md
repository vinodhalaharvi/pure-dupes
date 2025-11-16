# 🔍 pure-dupes

A blazingly fast, pure functional duplicate file finder powered by Merkle trees and parallel processing. Built with Go
and featuring a beautiful web-based UI.

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## ✨ Features

- **🌳 Merkle Tree-Based Deduplication** - Uses cryptographic hashing for accurate file comparison
- **⚡ Parallel Processing** - Leverages all CPU cores for maximum performance
- **🎯 Exact & Partial Match Detection** - Finds both identical files and similar content
- **🎨 Beautiful Web UI** - Interactive React-based interface with real-time filtering
- **🔍 Smart Filtering** - Filter by duplicate type, search by name, navigate with keyboard shortcuts
- **📊 Comprehensive Statistics** - Track space savings, duplicate counts, and more
- **🚀 Zero Dependencies** - Pure Go implementation with stdlib only
- **💻 Cross-Platform** - Works on Linux, macOS, and Windows

## 🎥 Demo

## 📦 Installation

```bash
go install github.com/vinodhalaharvi/pure-dupes@latest
pure-dupes
```

## ⚙️ Configuration Options

### Directory Path

- **What**: The root directory to scan
- **Example**: `/Users/john/Documents`
- **Tip**: Use absolute paths for clarity

### Similarity Threshold

- **Range**: 0.0 to 1.0 (0% to 100%)
- **Default**: 0.8 (80%)
- **Description**: Minimum similarity to consider files as partial duplicates
- **Examples**:
    - `1.0` = Only exact duplicates
    - `0.9` = Very similar files (90%+ matching chunks)
    - `0.7` = Moderately similar files
    - `0.5` = Loosely similar files

### Max Depth

- **Default**: 10
- **Description**: Maximum subdirectory levels to scan
- **Examples**:
    - `0` = Only files in the root directory
    - `1` = Root + immediate subdirectories
    - `5` = Scan 5 levels deep
    - `999` = Scan entire tree (use with caution on large directories)

### Worker Threads

- **Default**: Number of CPU cores
- **Description**: Parallel workers for file processing
- **Recommendation**: Leave at default for optimal performance
- **Range**: 1 to (2 × CPU cores)

### Chunk Size

- **Default**: 4096 bytes (4KB)
- **Description**: Size of data chunks for hashing
- **Considerations**:
    - Smaller chunks: More granular matching, higher memory usage
    - Larger chunks: Faster processing, less granular matching
- **Recommended values**: 1024, 2048, 4096, 8192

## 🧠 How It Works

### Merkle Tree Deduplication

Pure-dupes uses a sophisticated Merkle tree approach for file deduplication:

1. **Chunking**: Each file is divided into fixed-size chunks
2. **Hashing**: Every chunk is hashed using SHA-256
3. **Tree Building**: Hashes are organized into a Merkle tree
4. **Root Comparison**: Files with identical Merkle roots are exact duplicates
5. **Chunk Indexing**: A global chunk index enables fast partial match detection
6. **Similarity Calculation**: Shared chunks between files determine similarity percentage

**Benefits:**

- ✅ Content-based comparison (not just filenames)
- ✅ Efficient partial duplicate detection
- ✅ Cryptographically secure hashing
- ✅ Memory-efficient for large files

### Parallel Processing

- Automatically scales to available CPU cores
- Worker pool pattern for efficient task distribution
- Lock-free data structures where possible
- Optimized for both I/O and CPU-bound operations

### Optimization Tips

1. **Adjust Worker Count**: Match your CPU cores
2. **Tune Chunk Size**: Balance memory vs. granularity
3. **Limit Depth**: Reduce scope for faster results
4. **Use SSD**: I/O speed significantly impacts performance

### Code Architecture

**Key Components:**

1. **Thunks & Monoids** - Functional abstractions
2. **Merkle Tree** - Core deduplication algorithm
3. **File Processing** - Parallel file scanning and hashing
4. **Deduplication Engine** - Match detection and analysis
5. **HTTP Server** - REST API and embedded UI
6. **React Frontend** - Interactive web interface

## 📝 Use Cases

- **🧹 Disk Cleanup**: Find and remove duplicate files
- **📦 Backup Verification**: Ensure backups are complete
- **🎵 Media Libraries**: Deduplicate photos, music, videos
- **💼 Document Management**: Clean up document folders
- **🗄️ Archive Organization**: Optimize large archives
- **☁️ Cloud Storage**: Reduce cloud storage costs
- **🔬 Research Data**: Identify duplicate datasets
- **📸 Photo Collections**: Find duplicate or similar images

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Built with ❤️ using Go and React
- Inspired by functional programming principles
- Merkle trees for efficient comparison

## 📞 Support

- 🐛 **Issues**: [GitHub Issues](https://github.com/vinodhalaharvi/pure-dupes/issues)
- 📧 **Email**: vinod.halaharvi@gmail.com

## 🗺️ Roadmap

- [ ] Remote scanning over SSH/FTP

---

**Made with 🌳 and ⚡ by the pure-dupes team**