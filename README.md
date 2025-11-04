# Sistema de Leilão com Fechamento Automático

Sistema de leilão desenvolvido em Go com funcionalidade de fechamento automático baseado em tempo, utilizando goroutines para processamento concorrente.

## Funcionalidades

- ✅ Criação e gestão de leilões
- ✅ Sistema de lances em tempo real
- ✅ **Fechamento automático de leilões** após tempo definido
- ✅ Validação de leilões ativos/encerrados
- ✅ Processamento concorrente com goroutines
- ✅ Controle de concorrência thread-safe

## Tecnologias

- Go 1.22+
- MongoDB
- Docker & Docker Compose
- Goroutines para concorrência

## Como Funciona o Fechamento Automático

O sistema implementa um monitor de leilões expirados que:

1. **Inicia automaticamente**: Quando o `AuctionRepository` é criado, uma goroutine é iniciada para monitorar leilões
2. **Verifica periodicamente**: A cada minuto (ou metade da duração do leilão, o que for menor) verifica leilões expirados
3. **Fecha automaticamente**: Atualiza o status de `Active` para `Completed` para todos os leilões que ultrapassaram o tempo limite
4. **Thread-safe**: Usa operações atômicas do MongoDB (`UpdateMany`) para evitar race conditions

### Cálculo de Duração

A duração do leilão é configurada através da variável de ambiente `AUCTION_DURATION`:

- Formato: `10m`, `1h`, `30s`, etc (formato Go duration)
- Padrão: `5m` (5 minutos) se não especificado
- Compatibilidade: Também aceita `AUCTION_INTERVAL` (mantém compatibilidade com código existente)

## Como Executar

### Pré-requisitos

- Docker e Docker Compose instalados
- Porta 8080 (API) e 27017 (MongoDB) disponíveis

### 1. Clone o repositório

```bash
git clone git@github.com:robertocorreajr/fullcycle-desafio-leilao.git
cd fullcycle-desafio-leilao
```

### 2. Configure as variáveis de ambiente (opcional)

Edite o arquivo `cmd/auction/.env`:

```env
# Duração do leilão (20 segundos para teste, recomenda-se valores maiores em produção)
AUCTION_DURATION=20s

# Intervalo para inserção em batch de lances
BATCH_INSERT_INTERVAL=20s
MAX_BATCH_SIZE=4

# Configurações do MongoDB
MONGO_INITDB_ROOT_USERNAME=admin
MONGO_INITDB_ROOT_PASSWORD=admin
MONGODB_URL=mongodb://admin:admin@mongodb:27017/auctions?authSource=admin
MONGODB_DB=auctions
```

### 3. Suba os containers

```bash
docker compose up --build
```

A API estará disponível em: `http://localhost:8080`

## Endpoints da API

### Criar Leilão

```bash
POST /auction
Content-Type: application/json

{
  "product_name": "Notebook Dell",
  "category": "Electronics",
  "description": "Notebook Dell Inspiron 15, i7, 16GB RAM",
  "condition": 1
}
```

Condições (`condition`):
- `1`: Novo
- `2`: Usado
- `3`: Recondicionado

### Buscar Leilões

```bash
GET /auction?status=0&category=Electronics

# status: 0 = Active, 1 = Completed
```

### Buscar Leilão por ID

```bash
GET /auction/{auctionId}
```

### Criar Lance

```bash
POST /bid
Content-Type: application/json

{
  "user_id": "user123",
  "auction_id": "auction-id-here",
  "amount": 1500.00
}
```

### Buscar Lances

```bash
GET /bid/{auctionId}
```

## Executar Testes

### Rodar todos os testes

```bash
docker compose up -d mongodb
go test ./... -v
```

### Rodar teste específico de fechamento automático

```bash
docker compose up -d mongodb
go test ./internal/infra/database/auction -v -run TestAuctionAutoClose
```

Este teste:
- Cria um leilão com duração de 2 segundos
- Verifica que está com status `Active`
- Aguarda 3 segundos
- Verifica que foi fechado automaticamente para status `Completed`

## Estrutura do Projeto

```
.
├── cmd/
│   └── auction/
│       ├── main.go                 # Entry point da aplicação
│       └── .env                    # Variáveis de ambiente
├── configuration/
│   ├── database/                   # Configuração MongoDB
│   ├── logger/                     # Logger
│   └── rest_err/                   # Tratamento de erros
├── internal/
│   ├── entity/                     # Entidades de domínio
│   │   ├── auction_entity/
│   │   ├── bid_entity/
│   │   └── user_entity/
│   ├── infra/
│   │   ├── api/                    # Controllers e validação
│   │   └── database/
│   │       └── auction/
│   │           ├── create_auction.go        # ⭐ Implementação do fechamento automático
│   │           ├── create_auction_test.go   # ⭐ Testes do fechamento automático
│   │           └── find_auction.go
│   └── usecase/                    # Casos de uso
├── docker-compose.yml
├── Dockerfile
└── README.md
```

## Implementação do Fechamento Automático

A implementação está em `internal/infra/database/auction/create_auction.go` e inclui:

### 1. Função `getAuctionDuration()`

Calcula a duração do leilão baseada em variáveis de ambiente:

```go
func getAuctionDuration() time.Duration {
    auctionDuration := os.Getenv("AUCTION_DURATION")
    if auctionDuration == "" {
        auctionDuration = os.Getenv("AUCTION_INTERVAL")
    }
    
    duration, err := time.ParseDuration(auctionDuration)
    if err != nil {
        return time.Minute * 5 // default
    }
    
    return duration
}
```

### 2. Goroutine `monitorExpiredAuctions()`

Monitora continuamente leilões expirados:

```go
func (ar *AuctionRepository) monitorExpiredAuctions(ctx context.Context) {
    auctionDuration := getAuctionDuration()
    ticker := time.NewTicker(min(time.Minute, auctionDuration/2))
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            ar.closeExpiredAuctions(context.Background(), auctionDuration)
        }
    }
}
```

### 3. Função `closeExpiredAuctions()`

Fecha leilões que ultrapassaram o tempo limite:

```go
func (ar *AuctionRepository) closeExpiredAuctions(ctx context.Context, auctionDuration time.Duration) {
    expirationTime := time.Now().Add(-auctionDuration).Unix()
    
    filter := bson.M{
        "status":    auction_entity.Active,
        "timestamp": bson.M{"$lte": expirationTime},
    }
    
    update := bson.M{
        "$set": bson.M{
            "status": auction_entity.Completed,
        },
    }
    
    result, err := ar.Collection.UpdateMany(ctx, filter, update)
    // ... tratamento de erro e logging
}
```

## Controle de Concorrência

A solução garante thread-safety através de:

- **MongoDB UpdateMany**: Operação atômica que garante consistência
- **Context**: Permite cancelamento gracioso da goroutine
- **Ticker**: Intervalos regulares e controláveis de verificação
- **Integração com bid**: Sistema de bids já valida leilões expirados usando mutex

## Observações

- A goroutine de monitoramento é iniciada automaticamente quando o `AuctionRepository` é criado
- Em produção, recomenda-se usar durações maiores (ex: 1h, 1d)
- Para testes, pode-se usar durações curtas (ex: 20s, 1m)
- O sistema verifica a cada minuto ou metade da duração, o que for menor

## Troubleshooting

### Leilões não estão fechando automaticamente

1. Verifique a variável `AUCTION_DURATION` no arquivo `.env`
2. Verifique os logs para mensagens como "Auction expiration monitor started"
3. Certifique-se que o MongoDB está acessível
4. Verifique se há erros nos logs relacionados ao update do MongoDB

### Testes falhando

1. Certifique-se que o MongoDB está rodando: `docker compose up -d mongodb`
2. Verifique se a porta 27017 está disponível
3. Execute os testes com `-v` para ver logs detalhados

---

*Projeto desenvolvido como desafio Full Cycle - Go Expert*
