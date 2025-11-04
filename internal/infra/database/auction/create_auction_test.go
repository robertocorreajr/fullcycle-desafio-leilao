package auction

import (
"context"
"fullcycle-auction_go/internal/entity/auction_entity"
"os"
"testing"
"time"

"go.mongodb.org/mongo-driver/bson"
"go.mongodb.org/mongo-driver/mongo"
"go.mongodb.org/mongo-driver/mongo/options"
)

func setupTestDB(t *testing.T) (*mongo.Database, func()) {
	ctx := context.Background()
	
	// Conecta ao MongoDB de teste
	mongoURL := os.Getenv("MONGODB_URL")
	if mongoURL == "" {
		mongoURL = "mongodb://admin:admin@localhost:27017/auctions_test?authSource=admin"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURL))
	if err != nil {
		t.Fatalf("Failed to connect to MongoDB: %v", err)
	}

	dbName := "auctions_test"
	db := client.Database(dbName)

	// Cleanup function
	cleanup := func() {
		db.Collection("auctions").Drop(ctx)
		client.Disconnect(ctx)
	}

	return db, cleanup
}

func TestAuctionAutoClose(t *testing.T) {
	// Define duração curta para o teste (2 segundos)
	os.Setenv("AUCTION_DURATION", "2s")
	defer os.Unsetenv("AUCTION_DURATION")

	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Cria repositório (que inicia a goroutine de monitoramento)
	repo := NewAuctionRepository(db)

	// Cria um leilão de teste
	auction, _ := auction_entity.CreateAuction(
"Test Product",
"Electronics",
"A test product for auction",
auction_entity.New,
)

	ctx := context.Background()
	err := repo.CreateAuction(ctx, auction)
	if err != nil {
		t.Fatalf("Failed to create auction: %v", err)
	}

	// Verifica que o leilão foi criado com status Active
	var auctionMongo AuctionEntityMongo
	filter := bson.M{"_id": auction.Id}
	err2 := repo.Collection.FindOne(ctx, filter).Decode(&auctionMongo)
	if err2 != nil {
		t.Fatalf("Failed to find created auction: %v", err2)
	}

	if auctionMongo.Status != auction_entity.Active {
		t.Errorf("Expected auction status to be Active, got %d", auctionMongo.Status)
	}

	// Aguarda um pouco mais que a duração do leilão
	t.Log("Waiting for auction to expire...")
	time.Sleep(3 * time.Second)

	// Verifica se o leilão foi fechado automaticamente
	err3 := repo.Collection.FindOne(ctx, filter).Decode(&auctionMongo)
	if err3 != nil {
		t.Fatalf("Failed to find auction after expiration: %v", err3)
	}

	if auctionMongo.Status != auction_entity.Completed {
		t.Errorf("Expected auction status to be Completed after expiration, got %d", auctionMongo.Status)
	}

	t.Log("Auction was successfully closed automatically")
}

func TestGetAuctionDuration(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected time.Duration
	}{
		{
			name:     "Valid duration",
			envValue: "10m",
			expected: 10 * time.Minute,
		},
		{
			name:     "Invalid duration falls back to default",
			envValue: "invalid",
			expected: 5 * time.Minute,
		},
		{
			name:     "Empty env falls back to default",
			envValue: "",
			expected: 5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
if tt.envValue != "" {
os.Setenv("AUCTION_DURATION", tt.envValue)
defer os.Unsetenv("AUCTION_DURATION")
}

duration := getAuctionDuration()
			if duration != tt.expected {
				t.Errorf("Expected duration %v, got %v", tt.expected, duration)
			}
		})
	}
}

func TestCloseExpiredAuctions(t *testing.T) {
	os.Setenv("AUCTION_DURATION", "1s")
	defer os.Unsetenv("AUCTION_DURATION")

	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewAuctionRepository(db)
	ctx := context.Background()

	// Cria 2 leilões: um expirado e um ativo
	expiredAuction, _ := auction_entity.CreateAuction(
"Expired Product",
"Electronics",
"This auction should expire",
auction_entity.New,
)
	// Modifica o timestamp para ser no passado
	expiredAuction.Timestamp = time.Now().Add(-2 * time.Second)

	activeAuction, _ := auction_entity.CreateAuction(
"Active Product",
"Electronics",
"This auction should remain active",
auction_entity.New,
)

	repo.CreateAuction(ctx, expiredAuction)
	repo.CreateAuction(ctx, activeAuction)

	// Executa o fechamento manualmente
	repo.closeExpiredAuctions(ctx, 1*time.Second)

	// Verifica o leilão expirado
	var expiredMongo AuctionEntityMongo
	repo.Collection.FindOne(ctx, bson.M{"_id": expiredAuction.Id}).Decode(&expiredMongo)
	if expiredMongo.Status != auction_entity.Completed {
		t.Errorf("Expected expired auction to be Completed, got %d", expiredMongo.Status)
	}

	// Verifica o leilão ativo
	var activeMongo AuctionEntityMongo
	repo.Collection.FindOne(ctx, bson.M{"_id": activeAuction.Id}).Decode(&activeMongo)
	if activeMongo.Status != auction_entity.Active {
		t.Errorf("Expected active auction to remain Active, got %d", activeMongo.Status)
	}
}
