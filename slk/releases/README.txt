SLK Bank — Decentralized P2P Banking
=====================================

LINUX:
  chmod +x SLKBank-linux-amd64
  ./SLKBank-linux-amd64

WINDOWS / MAC — Build from source:
  1. Install Go 1.21+ from https://go.dev
  2. Install dependencies:
     Linux:   sudo apt install libgl1-mesa-dev xorg-dev
     Mac:     xcode-select --install
     Windows: Install TDM-GCC
  3. git clone https://github.com/slkproject/slk
  4. cd slk && go run ./cmd/slkbank/

NETWORK:
  - Connects automatically to 90+ peers worldwide
  - No account needed — just run and go
  - Your data is stored locally at ~/.slkbank/

