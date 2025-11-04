package auction

import (
"context"
"fullcycle-auction_go/configuration/logger"
"fullcycle-auction_go/internal/entity/auction_entity"
"fullcycle-auction_go/internal/internal_error"
"os"
"time"

"go.mongodb.org/mongo-driver/bson"
"go.mongodb.org/mongo-driver/mongo"
)

type AuctionEntityMongo struct {
	Id          string                          `bson:"_id"`
	ProductName string                          `bson:"product_name"`
	Category    string                          `bson:"category"`
	Description string                          `bson:"description"`
	Condition   auction_entity.ProductCondition `bson:"condition"`
	Status      auction_entity.AuctionStatus    `bson:"status"`
	Timestamp   int64                           `bson:"timestamp"`
}

type AuctionRepository struct {
	Collection *mongo.Collection
}

func NewAuctionRepository(database *mongo.Database) *AuctionRepository {
	repo := &AuctionRepository{
		Collection: database.Collection("auctions"),
	}

	// Inicia a goroutine que monitora leilões expirados
	go repo.monitorExpiredAuctions(context.Background())

	return repo
}

func (ar *AuctionRepository) CreateAuction(
ctx context.Context,
auctionEntity *auction_entity.Auction) *internal_error.InternalError {
	auctionEntityMongo := &AuctionEntityMongo{
		Id:          auctionEntity.Id,
		ProductName: auctionEntity.ProductName,
		Category:    auctionEntity.Category,
		Description: auctionEntity.Description,
		Condition:   auctionEntity.Condition,
		Status:      auctionEntity.Status,
		Timestamp:   auctionEntity.Timestamp.Unix(),
	}
	_, err := ar.Collection.InsertOne(ctx, auctionEntityMongo)
	if err != nil {
		logger.Error("Error trying to insert auction", err)
		return internal_error.NewInternalServerError("Error trying to insert auction")
	}

	return nil
}

// getAuctionDuration retorna a duração do leilão baseada na variável de ambiente AUCTION_DURATION
// Se não estiver definida, retorna 5 minutos como padrão
func getAuctionDuration() time.Duration {
	auctionDuration := os.Getenv("AUCTION_DURATION")
	if auctionDuration == "" {
		auctionDuration = os.Getenv("AUCTION_INTERVAL") // Compatibilidade com código existente
	}

	duration, err := time.ParseDuration(auctionDuration)
	if err != nil {
		logger.Info("Using default auction duration of 5 minutes")
		return time.Minute * 5
	}

	return duration
}

// monitorExpiredAuctions é uma goroutine que verifica periodicamente leilões expirados
// e os fecha automaticamente
func (ar *AuctionRepository) monitorExpiredAuctions(ctx context.Context) {
	auctionDuration := getAuctionDuration()
	
	// Verifica a cada minuto ou a cada metade da duração do leilão (o que for menor)
	ticker := time.NewTicker(min(time.Minute, auctionDuration/2))
	defer ticker.Stop()

	logger.Info("Auction expiration monitor started")

	for {
		select {
		case <-ctx.Done():
			logger.Info("Auction expiration monitor stopped")
			return
		case <-ticker.C:
			ar.closeExpiredAuctions(context.Background(), auctionDuration)
		}
	}
}

// closeExpiredAuctions busca e fecha todos os leilões que já expiraram
func (ar *AuctionRepository) closeExpiredAuctions(ctx context.Context, auctionDuration time.Duration) {
	// Calcula o timestamp de expiração (agora - duração do leilão)
	expirationTime := time.Now().Add(-auctionDuration).Unix()

	// Filtro para buscar leilões ativos que já expiraram
	filter := bson.M{
		"status":    auction_entity.Active,
		"timestamp": bson.M{"$lte": expirationTime},
	}

	// Update para marcar como completo
	update := bson.M{
		"$set": bson.M{
			"status": auction_entity.Completed,
		},
	}

	// Atualiza todos os leilões que correspondem ao filtro
	result, err := ar.Collection.UpdateMany(ctx, filter, update)
	if err != nil {
		logger.Error("Error trying to close expired auctions", err)
		return
	}

	if result.ModifiedCount > 0 {
		logger.Info("Closed expired auctions")
	}
}

// helper function para min
func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
