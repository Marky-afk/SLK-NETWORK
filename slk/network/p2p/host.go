package p2p

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/multiformats/go-multiaddr"
)

const (
	TopicTrophies     = "slk-trophies"
	TopicRacers       = "slk-racers"
	TopicTransactions = "slk-transactions"
	NetworkRendezvous = "slk-proof-of-race-mainnet-v1"
	nodeKeyFile       = "node_key"
)

var BootstrapPeers = []string{
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
	"/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",
}

type TrophyMsg struct {
	Winner   string  `json:"winner"`
	Distance float64 `json:"distance"`
	Time     float64 `json:"time"`
	Tier     int     `json:"tier"`
	Hash     string  `json:"hash"`
	PrevHash string  `json:"prev_hash"`
	Height   uint64  `json:"height"`
	VDFProof string  `json:"vdf_proof"`
	VDFInput string  `json:"vdf_input"`
}

type TxMsg struct {
	ID        string  `json:"id"`
	From      string  `json:"from"`
	To        string  `json:"to"`
	Amount    float64 `json:"amount"`
	Timestamp int64   `json:"timestamp"`
	Signature string  `json:"signature"`
	PubKey    string  `json:"pub_key"`
	Type      int     `json:"type"`
}

type RacerMsg struct {
	Address      string  `json:"address"`
	DistanceLeft float64 `json:"distance_left"`
	Power        float64 `json:"power"`
	Temp         float64 `json:"temp"`
	Status       string  `json:"status"`
	Username     string  `json:"username"`
}

type Node struct {
	Host        host.Host
	DHT         *dht.IpfsDHT
	PubSub      *pubsub.PubSub
	TrophyTopic *pubsub.Topic
	RacerTopic  *pubsub.Topic
	TrophySub   *pubsub.Subscription
	RacerSub    *pubsub.Subscription
	TxTopic     *pubsub.Topic
	TxSub       *pubsub.Subscription
	Ctx         context.Context
	Cancel      context.CancelFunc
	PeerCount   int
	OnTrophy    func(TrophyMsg)
	OnRacer     func(RacerMsg)
	OnTx        func(TxMsg)
}

func loadOrCreateKey(dataDir string) (crypto.PrivKey, error) {
	keyPath := filepath.Join(dataDir, nodeKeyFile)
	if data, err := os.ReadFile(keyPath); err == nil {
		priv, err := crypto.UnmarshalPrivateKey(data)
		if err == nil {
			return priv, nil
		}
	}
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.Ed25519, 256, rand.Reader)
	if err != nil {
		return nil, err
	}
	data, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	os.MkdirAll(dataDir, 0700)
	return priv, os.WriteFile(keyPath, data, 0600)
}

func NewNode(port int, dataDir string) (*Node, error) {
	ctx, cancel := context.WithCancel(context.Background())

	privKey, err := loadOrCreateKey(dataDir)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to load/create node key: %w", err)
	}

	publicIP := "41.90.70.28"
	extMultiaddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%d", publicIP, port))

	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port),
			fmt.Sprintf("/ip6/::/tcp/%d", port),
		),
		libp2p.AddrsFactory(func(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
			if extMultiaddr != nil {
				addrs = append(addrs, extMultiaddr)
			}
			return addrs
		}),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
		libp2p.EnableHolePunching(),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create host: %w", err)
	}

	kdht, err := dht.New(ctx, h, dht.Mode(dht.ModeAutoServer))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create DHT: %w", err)
	}

	if err := kdht.Bootstrap(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create pubsub: %w", err)
	}

	trophyTopic, err := ps.Join(TopicTrophies)
	if err != nil {
		cancel()
		return nil, err
	}
	trophySub, err := trophyTopic.Subscribe()
	if err != nil {
		cancel()
		return nil, err
	}

	racerTopic, err := ps.Join(TopicRacers)
	if err != nil {
		cancel()
		return nil, err
	}
	racerSub, err := racerTopic.Subscribe()
	if err != nil {
		cancel()
		return nil, err
	}

	txTopic, err := ps.Join(TopicTransactions)
	if err != nil {
		cancel()
		return nil, err
	}
	txSub, err := txTopic.Subscribe()
	if err != nil {
		cancel()
		return nil, err
	}

	node := &Node{
		Host:        h,
		DHT:         kdht,
		PubSub:      ps,
		TrophyTopic: trophyTopic,
		RacerTopic:  racerTopic,
		TrophySub:   trophySub,
		RacerSub:    racerSub,
		TxTopic:     txTopic,
		TxSub:       txSub,
		Ctx:         ctx,
		Cancel:      cancel,
	}

	return node, nil
}

func (n *Node) Start() {
	fmt.Println("\n🌐 P2P NODE STARTED")
	fmt.Printf("🔑 Peer ID: %s\n", n.Host.ID())
	fmt.Println("📡 Your node addresses:")
	for _, addr := range n.Host.Addrs() {
		fmt.Printf("   %s/p2p/%s\n", addr, n.Host.ID())
	}
	fmt.Println("")
	fmt.Println("🔗 SHARE THIS ADDRESS WITH OTHERS TO JOIN YOUR NETWORK:")
	fmt.Printf("   /ip4/41.90.70.28/tcp/30303/p2p/%s\n\n", n.Host.ID())
	go n.connectBootstrap()
	go n.discoverPeers()
	go n.listenTrophies()
	go n.listenRacers()
	go n.listenTxs()
	go n.trackPeers()
}

func (n *Node) connectBootstrap() {
	time.Sleep(2 * time.Second)
	connected := 0
	for _, addrStr := range BootstrapPeers {
		addr, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			continue
		}
		peerInfo, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			continue
		}
		if peerInfo.ID == n.Host.ID() {
			continue
		}
		if err := n.Host.Connect(n.Ctx, *peerInfo); err != nil {
			// silent fail
		} else {
			// silent bootstrap
			connected++
		}
	}
	if connected == 0 {
		fmt.Println("⚠️  Could not reach bootstrap peers. Check your internet connection.")
	} else {
		// silent live
	}
}

func (n *Node) discoverPeers() {
	rd := drouting.NewRoutingDiscovery(n.DHT)
	dutil.Advertise(n.Ctx, rd, NetworkRendezvous)

	ticker := time.NewTicker(30 * time.Second)
	for {
		select {
		case <-ticker.C:
			peerChan, err := dutil.FindPeers(n.Ctx, rd, NetworkRendezvous)
			if err != nil {
				continue
			}
			for _, p := range peerChan {
				if p.ID == n.Host.ID() {
					continue
				}
				if n.Host.Network().Connectedness(p.ID) != network.Connected {
					n.Host.Connect(n.Ctx, p)
				}
			}
		case <-n.Ctx.Done():
			return
		}
	}
}

func (n *Node) trackPeers() {
	ticker := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-ticker.C:
			n.PeerCount = len(n.Host.Network().Peers())
		case <-n.Ctx.Done():
			return
		}
	}
}

func (n *Node) listenTrophies() {
	for {
		msg, err := n.TrophySub.Next(n.Ctx)
		if err != nil {
			return
		}
		if msg.ReceivedFrom == n.Host.ID() {
			continue
		}
		var t TrophyMsg
		if err := json.Unmarshal(msg.Data, &t); err != nil {
			continue
		}
		if n.OnTrophy != nil {
			n.OnTrophy(t)
		}
	}
}

func (n *Node) listenRacers() {
	for {
		msg, err := n.RacerSub.Next(n.Ctx)
		if err != nil {
			return
		}
		if msg.ReceivedFrom == n.Host.ID() {
			continue
		}
		var r RacerMsg
		if err := json.Unmarshal(msg.Data, &r); err != nil {
			continue
		}
		if n.OnRacer != nil {
			n.OnRacer(r)
		}
	}
}

func (n *Node) BroadcastTrophy(t TrophyMsg) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return n.TrophyTopic.Publish(n.Ctx, data)
}

func (n *Node) BroadcastRacerPosition(r RacerMsg) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return n.RacerTopic.Publish(n.Ctx, data)
}

func (n *Node) listenTxs() {
	for {
		msg, err := n.TxSub.Next(n.Ctx)
		if err != nil {
			return
		}
		if msg.ReceivedFrom == n.Host.ID() {
			continue
		}
		var tx TxMsg
		if err := json.Unmarshal(msg.Data, &tx); err != nil {
			continue
		}
		if n.OnTx != nil {
			n.OnTx(tx)
		}
	}
}

func (n *Node) BroadcastTx(tx TxMsg) error {
	data, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	return n.TxTopic.Publish(n.Ctx, data)
}

func (n *Node) Stop() {
	n.Cancel()
	n.Host.Close()
}

func (n *Node) PeerID() string {
	return n.Host.ID().String()
}

// ─── CHAIN SYNC ────────────────────────────────────────────────────────────

const SyncProtocol = "/slk/chainsync/1.0.0"

// ChainRequest — sent by a new node asking for trophies above a certain height
type ChainRequest struct {
	FromHeight uint64 `json:"from_height"`
}

// ChainResponse — sent back with all trophies above that height
type ChainResponse struct {
	Trophies []TrophyMsg `json:"trophies"`
	Height   uint64      `json:"height"`
}

// OnChainRequest is called when a peer asks for our chain.
// main.go sets this to a function that reads bc.Trophies and returns them.
var OnChainRequest func(fromHeight uint64) ChainResponse

// ServeChainSync registers the handler so peers can request our chain
func (n *Node) ServeChainSync() {
	n.Host.SetStreamHandler(SyncProtocol, func(s network.Stream) {
		defer s.Close()
		var req ChainRequest
		if err := json.NewDecoder(s).Decode(&req); err != nil {
			return
		}
		if OnChainRequest == nil {
			return
		}
		resp := OnChainRequest(req.FromHeight)
		json.NewEncoder(s).Encode(resp)
	})
}

// RequestChainFrom asks a specific peer for trophies above fromHeight
func (n *Node) RequestChainFrom(peerID peer.ID, fromHeight uint64) (*ChainResponse, error) {
	s, err := n.Host.NewStream(n.Ctx, peerID, SyncProtocol)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	req := ChainRequest{FromHeight: fromHeight}
	if err := json.NewEncoder(s).Encode(req); err != nil {
		return nil, err
	}
	var resp ChainResponse
	if err := json.NewDecoder(s).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SyncWithBestPeer finds the peer with the most trophies and syncs from them
func (n *Node) SyncWithBestPeer(ourHeight uint64) (*ChainResponse, error) {
	peers := n.Host.Network().Peers()
	for _, p := range peers {
		resp, err := n.RequestChainFrom(p, ourHeight)
		if err != nil {
			continue
		}
		if resp.Height > ourHeight {
			return resp, nil
		}
	}
	return nil, fmt.Errorf("no peer has a longer chain")
}
