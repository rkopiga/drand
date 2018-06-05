package core

import (
	"fmt"
	"os"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/dedis/drand/beacon"
	"github.com/dedis/drand/key"
	"github.com/dedis/drand/test"
	"github.com/dedis/kyber/sign/bls"
	"github.com/nikkolasg/slog"
	"github.com/stretchr/testify/require"
	//"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/assert"
)

func TestDrandDKG(t *testing.T) {
	slog.Level = slog.LevelDebug

	n := 5
	nbBeacons := 3
	//thr := key.DefaultThreshold(n)
	period := 700 * time.Millisecond

	drands := BatchNewDrand(n,
		WithBeaconPeriod(period))
	defer CloseAllDrands(drands)

	var wg sync.WaitGroup
	wg.Add(n - 1)
	for _, drand := range drands[1:] {
		go func(d *Drand) {
			err := d.WaitDKG()
			require.Nil(t, err)
			wg.Done()
		}(drand)
	}

	root := drands[0]
	err := root.StartDKG()
	require.Nil(t, err)
	wg.Wait()

	// check if share + dist public files are saved
	public, err := root.store.LoadDistPublic()
	require.Nil(t, err)
	require.NotNil(t, public)
	pri, err := root.store.LoadShare()
	require.Nil(t, err)


	rdkg := make([]*Drand,n)
	for i := 0; i < n; i++{
		rdkg[i] , _ = drands[i].RefreshDKG()
		}


	var wgd sync.WaitGroup
	wgd.Add(n)
	for _, drand := range rdkg{
		go func(d *Drand) {
			err := d.RenewalDKG()
			fmt.Print("REFRESHING DONE")
			require.Nil(t,err)
			wgd.Done()
		}(drand)
	}

	r := rdkg[0]
	erir := r.StartRefresh()
	require.Nil(t,erir)
	wgd.Wait()
	//erir := r.StartRefresh()
	//require.Nil(t,erir)
	//wgd.Wait()
	pub, _ := r.store.LoadDistPublic()
	piv, _ := r.store.LoadShare()

	assert.Equal(t,public,pub)
	assert.Equal(t,piv,pri)

	receivedChan := make(chan int, nbBeacons*n)
	//expected := nbBeacons * n
	// launchBeacon will launch the beacon at the given index. Each time a new
	// beacon is ready from that node, it indicates it by sending the index on
	// the receivedChan channel.
	launchBeacon := func(i int) {
		curr := 0
		myCb := func(b *beacon.Beacon) {
			err := bls.Verify(key.Pairing, public.Key, beacon.Message(b.PreviousRand, b.Round), b.Randomness)
			require.NoError(t, err)
			curr++
			//slog.Printf(" --- TEST: new beacon generated (%d/%d) from node %d", curr, nbBeacons, i)
			receivedChan <- i
		}
		//fmt.Printf(" --- TEST: callback for node %d: %p\n", i, myCb)
		drands[i].opts.beaconCbs = append(drands[i].opts.beaconCbs, myCb)
		go drands[i].BeaconLoop()
	}

	for i := 0; i < n; i++ {
		launchBeacon(i)
	}

	done := make(chan bool)
	// keep track of how many do we have
	go func() {
		receivedIdx := make(map[int]int)
		for {
			receivedIdx[<-receivedChan]++
			var continueRcv = false
			for i := 0; i < n; i++ {
				rcvd := receivedIdx[i]
				if rcvd < nbBeacons {
					continueRcv = true
					break
				}
			}
			if !continueRcv {
				done <- true
				return
			}
		}
	}()

	select {
	case <-done:
		fmt.Println("youpi")
	case <-time.After(period * time.Duration(nbBeacons*2)):
		t.Fatal("not in time")
	}

	client := NewClient(root.opts.grpcOpts...)
	//fmt.Printf("testing client functionality with public key %x\n", public.Key)
	resp, err := client.LastPublic(root.priv.Public.Addr, public)
	require.NoError(t, err)
	require.NotNil(t, resp)
}
/*
func TestDrand_RefreshDKGDKG(t *testing.T) {
	n := 2
	period := 700 * time.Millisecond
	dkgs := BatchNewDrand(n,WithBeaconPeriod(period))
	defer CloseAllDrands(dkgs)
	var wg sync.WaitGroup
	wg.Add(n - 1)
	for _, drand := range dkgs[1:] {
		go func(d *Drand) {
			err := d.WaitDKG()
			require.Nil(t, err)
			wg.Done()
		}(drand)
	}

	root := dkgs[0]
	err := root.StartDKG()
	require.Nil(t, err)
	wg.Wait()

	// check if share + dist public files are saved
	public, err := root.store.LoadDistPublic()
	require.Nil(t, err)
	require.NotNil(t, public)
	_, err = root.store.LoadShare()
	require.Nil(t, err)
}
*/

func BatchNewDrand(n int, opts ...ConfigOption) []*Drand {
	privs, group := test.BatchIdentities(n)
	var err error
	drands := make([]*Drand, n, n)
	tmp := os.TempDir()
	for i := 0; i < n; i++ {
		s := test.NewKeyStore()
		s.SaveKeyPair(privs[i])
		// give each one their own private folder
		dbFolder := path.Join(tmp, fmt.Sprintf("db-%d", i))
		drands[i], err = NewDrand(s, group, NewConfig(append([]ConfigOption{WithDbFolder(dbFolder)}, opts...)...))
		if err != nil {
			panic(err)
		}
	}
	return drands
}

func CloseAllDrands(drands []*Drand) {
	for i := 0; i < len(drands); i++ {
		drands[i].Stop()
		os.RemoveAll(drands[i].opts.dbFolder)
	}
}
