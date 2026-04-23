# SLK Proof-of-Race Blockchain

## Install (Ubuntu/WSL)
```bash
sudo apt update && sudo apt install golang gcc g++ stress-ng git -y
git clone https://github.com/Marky-afk/SLK-NETWORK.git
cd SLK-NETWORK/slk
go build ./cmd/slkd/
./slkd
```

## How to Mine
1. Run `./slkd`
2. Choose option `1` to start racing
3. Press `Q` + ENTER to stop
4. Press `S` + ENTER to cool down
5. Press `F` + ENTER for full speed

## Explorer
```bash
php -S 0.0.0.0:9000 slk_explorer.php
```
Open http://localhost:9000
