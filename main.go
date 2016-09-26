package main

import (
	//"fmt"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"github.com/crackcomm/cloudflare"
	"golang.org/x/net/context"
	"gopkg.in/yaml.v2"
)

type Stats struct {
	Created int
	Deleted int
	Updated int
	Elapsed time.Duration
}

type ChiefRecord struct {
	Name   string
	Value  string
	Type   string
	TTL    int
	State  string
	record *cloudflare.Record
}

func main() {
	startTime := time.Now()
	stats := Stats{}
	client := cloudflare.New(&cloudflare.Options{
		Email: os.Getenv("EMAIL"),
		Key:   os.Getenv("API_KEY"),
	})

	var zoneName string
	var zone *cloudflare.Zone
	var cmdSync bool
	var cmdImport bool

	flag.StringVar(&zoneName, "zone", "", "specify zone to manage")
	flag.BoolVar(&cmdImport, "import", false, "import : dump CF records to yaml")
	flag.BoolVar(&cmdSync, "sync", false, "sync : run local against remote")
	flag.Parse()

	if len(zoneName) == 0 {
		log.Fatal("-zone is required")
	}

	if cmdSync && cmdImport {
		log.Fatal("Only one command at a time, either -sync or -import")
	}

	ctx := context.Background()
	ctx, _ = context.WithTimeout(ctx, time.Second*30)

	zones, err := client.Zones.List(ctx)
	if err != nil {
		log.Fatal(err)
	} else if len(zones) == 0 {
		log.Fatal("No zones were found")
	}

	for _, z := range zones {
		if z.Name == zoneName {
			zone = z
		}
	}

	if zone == nil {
		log.Fatal("Zone not found: ", zoneName)
	}

	log.Println("Zone found:", zone.Name)

	// TODO: extract into method for refreshing records
	records, err := client.Records.List(ctx, zone.ID)
	if err != nil {
		log.Fatal(err)
	}
	chiefRecords := []ChiefRecord{}
	for _, record := range records {
		tmp := ChiefRecord{Name: record.Name, Value: record.Content,
			Type: record.Type, TTL: record.TTL, record: record, State: "present"}
		chiefRecords = append(chiefRecords, tmp)
	}

	log.Println(len(chiefRecords), "remote records found.")
	// end TODO

	if cmdImport {
		out, err := yaml.Marshal(&chiefRecords)
		if err != nil {
			log.Fatal(err)
		}
		err = ioutil.WriteFile("chief.yml", out, 0644)
		log.Println("records imported to chief.yml")
	}

	if cmdSync {
		// sync up differences - loop over local, verify in remote,
		// log msg of missing records in local
		// if not in remote but in local, add/delete depending on state cmd

		files, err := ioutil.ReadDir(".")
		if err != nil {
			log.Fatal("Error reading configs: ", err)
		}

		localRecords := []ChiefRecord{}

		for _, file := range files {
			fileMeta := strings.Split(file.Name(), ".")
			if len(fileMeta) >= 2 && fileMeta[len(fileMeta)-1] == "yml" {
				log.Println("[config] loading:", file.Name())
				chiefYML, err := ioutil.ReadFile(file.Name())
				if err != nil {
					log.Fatal(err)
				}

				tmp := []ChiefRecord{}
				err = yaml.Unmarshal(chiefYML, &tmp)
				localRecords = append(localRecords, tmp...)
				log.Println(len(tmp), "records loaded.")
			}
		}

		// validate state in records
		validConfig := true
		for _, r := range localRecords {
			if r.State == "present" || r.State == "absent" {
				continue
			}
			if r.State != "present" {
				validConfig = false
				log.Println("Invalid record state:", r.State, "for", r.Name)
			}
		}

		if !validConfig {
			log.Fatal("Invalid config.")
		}

		log.Println(len(localRecords), "local records found.")

		for _, r := range localRecords {
			if !exists(chiefRecords, r, zone) {
				log.Println("[creating]", r.Name, "(", r.Value, ") doesn't exist in remote.")
				createRecord(r, client, ctx, zone)
				stats.Created++
			}

			updated := false
			deleted := false

			if r.State == "present" {
				updated = checkPatch(chiefRecords, r, zone, client, ctx)
			}

			if r.State == "absent" {
				deleted = checkDelete(chiefRecords, r, zone, client, ctx)
			}

			if deleted {
				stats.Deleted++
			}
			if updated {
				stats.Updated++
			}
		}

		if len(localRecords) != len(chiefRecords) {
			log.Println("Consider running -operation=import to sync up differences.")
		}

		stats.Elapsed = time.Since(startTime)

		log.Printf("%+v\n", stats)
	}
}

func createRecord(record ChiefRecord, client *cloudflare.Client,
	ctx context.Context, zone *cloudflare.Zone) {

	cfRecord := &cloudflare.Record{Type: record.Type, Name: record.Name,
		Content: record.Value, TTL: record.TTL, ZoneID: zone.ID, ZoneName: zone.Name}
	err := client.Records.Create(ctx, cfRecord)
	if err != nil {
		log.Fatal("Error creating record: ", err)
	}

}

func checkDelete(remoteRecords []ChiefRecord, localRecord ChiefRecord,
	zone *cloudflare.Zone, client *cloudflare.Client, ctx context.Context) bool {

	fqdn := fmt.Sprintf("%s.%s", localRecord.Name, zone.Name)
	var remoteRecord *ChiefRecord

	for _, r := range remoteRecords {
		// just check for the name
		// dont match on value/ttl since that could be getting updated
		if r.Name == localRecord.Name || fqdn == r.Name {
			remoteRecord = &r
			break
		}
	}

	if remoteRecord == nil {
		log.Println("[delete] Cannot find remote record for:", localRecord.Name, "skipping.")
		return false
	}

	err := client.Records.Delete(ctx, zone.ID, remoteRecord.record.ID)
	if err != nil {
		log.Fatal("Error deleting record:", err)
	}
	log.Println("[deleted]", localRecord.Name)
	return true
}

func checkPatch(remoteRecords []ChiefRecord, localRecord ChiefRecord,
	zone *cloudflare.Zone, client *cloudflare.Client, ctx context.Context) bool {

	fqdn := fmt.Sprintf("%s.%s", localRecord.Name, zone.Name)
	var remoteRecord *ChiefRecord

	for _, r := range remoteRecords {
		// just check for the name
		// dont match on value/ttl since that could be getting updated
		if r.Name == localRecord.Name || fqdn == r.Name {
			remoteRecord = &r
			break
		}
	}

	if remoteRecord == nil {
		log.Println("[patch] Cannot find remote record for:", localRecord.Name)
		return false
	}

	patch := false

	if remoteRecord.Value != localRecord.Value {
		patch = true
		log.Println("[patching]", localRecord.Name, " :: value:", remoteRecord.Value, "->", localRecord.Value)
	}
	if remoteRecord.TTL != localRecord.TTL {
		patch = true
		log.Println("[patching]", localRecord.Name, " :: ttl:", remoteRecord.TTL, "->", localRecord.TTL)
	}
	if remoteRecord.Type != localRecord.Type {
		patch = true
		log.Println("[patching]", localRecord.Name, " :: type:", remoteRecord.Type, "->", localRecord.Type)
	}

	if patch {
		cfRecord := &cloudflare.Record{Type: localRecord.Type, Name: localRecord.Name, ID: remoteRecord.record.ID,
			Content: localRecord.Value, TTL: localRecord.TTL, ZoneID: zone.ID, ZoneName: zone.Name}
		err := client.Records.Patch(ctx, cfRecord)
		if err != nil {
			log.Fatal("Error patching record:", err)
		}
		log.Println("[patched]", localRecord.Name)
		return true
	}
	return false
}

func exists(records []ChiefRecord, record ChiefRecord, zone *cloudflare.Zone) bool {
	fqdn := fmt.Sprintf("%s.%s", record.Name, zone.Name)
	for _, r := range records {
		// just check for the name
		// dont match on value/ttl since that could be getting updated
		if r.Name == record.Name || fqdn == r.Name {
			return true
		}
	}
	return false
}
