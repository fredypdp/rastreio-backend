# Ulemo - Integra Bwé APIs (IBA) — Documentação

> Fonte: https://app.api-facil.dev/prd-doc/ulemo---integra-bwe--apis--iba--api-doc000021
> Publicada por Tonibart - Comércio e Serviços, (SU), LDA (https://tonibart.com) através de www.api-facil.dev
> Conteúdo transcrito integralmente da página acima, dividido em secções.

<!-- Logótipo da página: https://app.api-facil.dev/uploads/docs/logos/1781273560200...jpg (URL truncada na extração) -->

- **Versão actual:** V1.0.0
- **Categoria:** Sistema de Facturação e Gestão
- **Ult. actualização:** 12/06/2026 19:54
- **By:** TONIBART [Website](https://app.api-facil.dev/prd-doc/${documentacao.desenvolvedor.website})

---

## Índice

1. [Visão geral](#1-visão-geral)
2. [Autenticação](#2-autenticação)
   - [2.1. Cabeçalho](#21-cabeçalho)
3. [Dados constantes](#3-dados-constantes)
   - [3.2. Pacotes de licença](#32-pacotes-de-licença)
   - [3.3. Tipos de documentos](#33-tipos-de-documentos)
   - [3.4. Taxas & Imposto (IVA)](#34-taxas--imposto-iva)
   - [3.5. Isenções](#35-isenções)
   - [3.6. Grupos de artigo](#36-grupos-de-artigo)
   - [3.7. Tipos de descontos](#37-tipos-de-descontos)
   - [3.8. Motivo Nota de Crédito](#38-motivo-nota-de-crédito)
   - [3.9. Retenção na Fonte](#39-retenção-na-fonte)
4. [Comerciantes](#4-comerciantes)
   - [4.10. Criar comerciante](#410-criar-comerciante)
   - [4.11. Editar comerciante](#411-editar-comerciante)
   - [4.12. Listar Comerciantes](#412-listar-comerciantes)
   - [4.13. Consultar por ID](#413-consultar-por-id)
   - [4.14. Renovar Licença](#414-renovar-licença)
   - [4.15. Chaves F. electrónica](#415-chaves-f-electrónica)
5. [Estabelecimentos](#5-estabelecimentos)
   - [5.16. Criar estabelecimento](#516-criar-estabelecimento)
   - [5.17. Editar estabelecimento](#517-editar-estabelecimento)
   - [5.18. Apagar estabelecimento](#518-apagar-estabelecimento)
   - [5.19. Listar Estabelecimentos](#519-listar-estabelecimentos)
   - [5.20. Consultar por ID](#520-consultar-por-id)
6. [Utilizadores](#6-utilizadores)
   - [6.21. Criar utilizador](#621-criar-utilizador)
   - [6.22. Editar utilizador](#622-editar-utilizador)
   - [6.23. Apagar utilizador](#623-apagar-utilizador)
   - [6.24. Listar Utilizadores](#624-listar-utilizadores)
   - [6.25. Consultar por ID](#625-consultar-por-id)
7. [Documentos](#7-documentos)
   - [7.26. Parâmetros de entrada](#726-parâmetros-de-entrada)
   - [7.27. Criar factura](#727-criar-factura)
   - [7.28. Criar Recibo](#728-criar-recibo)
   - [7.29. Criar Nota de Crédito](#729-criar-nota-de-crédito)
   - [7.30. Consultar por ID](#730-consultar-por-id)
   - [7.31. Listagem por Posto](#731-listagem-por-posto)
8. [Erros & soluções](#8-erros--soluções)

---

## 1. Visão geral

O Plano **IBA - INTEGRA BWÉ APIs** permite que Distribuidores integrem a plataforma de facturação electrónica Ulemo nos seus próprios sistemas, gerindo múltiplas contas de comerciantes e revendendo o serviço com a sua própria marca.

Esta documentação orienta e simplifica a integração completa, esta instrue claramente a **criar** e **editar** comerciante, **renovar planos de licença**, efectuar CRUD de **utilizadores** e **estabelecimentos**, configuração de **chaves de facturação electrónica**, emissão e consulta de documentos.

> **Validação de NIF – Atenção!**
>
> Os documentos fiscais emitidos com NIF inválido de clientes serão automáticamente anulados após validação da AGT (que ocorre minutos após a emissão).
>
> ⚠️ Para evitar transtornos e garantir que **cliente final** receba um documento fiscal válido, informe sempre **NIFs** verdadeiros e correctos ou emita com NIF de *consumidor final* em situações que não haja certeza da identificação do contribuinte.

### **Número do Estabelecimento**

O campo **numeroEstabelecimento** nas requisições desta API, identifica a agência ou filial a partir da qual o documento está a ser emitido.

### Procedimento obrigatório (Para **empresas** com **mais** de uma **Filial**)

- Cadastrar o estabelecimento na AGT – através do Portal do Contribuinte
- Cadastrar o estabelecimento no Ulemo – via API ou Painel do distribuidor
- Utilizar o número nas requisições – após os passos anteriores

### Valor padrão para uma **Filial** ou **Testes de integração**

Em embiente *Development* ou para comerciantes com apenas uma **localização**, pode utilizar:

```
{ "numeroEstabelecimento": "SEDE" }
```

---

## 2. Autenticação

Todas as requisições relacionadas com **criação/edição de comerciantes**, **renovação** de licença, configurações de **chaves de facturação electrónica**, CRUD de **estabelecimentos** e **utilizadores**, utilizam apenas *API-Key-Distribuidor* no seu header, que identifica o Distribuidor.

Requisições relacionadas com **emissão e consulta de documentos**, utilizam *API-Key-Distribuidor* e *API-Key-Comerciante* no seu header, para identificação do Distribuidor e Comerciante da operação.

### 2.1. Cabeçalho

#### Base URL

```
https://iba.ulemo.ao/api/integration/v1
```

#### Headers obrigatórios para:

#### 1. *Criação/edição de comerciantes;* 2. *Renovação de licença;* 3. *Configuração de chaves de facturação electrónica;* 4. *CRUD de Estabelecimentos, Utilizadores.*

| Header | Valor | Descrição |
| --- | --- | --- |
| API-Key-Distribuidor | {sua_chave_aqui} | Chave única fornecida pela Ulemo |

#### Headers obrigatórios para **Emissão** & **Consulta** de **Documento**

| Header | Valor | Descrição |
| --- | --- | --- |
| API-Key-Distribuidor | {sua_chave_aqui} | Chave única fornecida pela Ulemo |
| API-Key-Comerciante | {sua_chave_aqui} | Chave única fornecida pela Ulemo |
| Environment | Development ou Production | Define se os dados são de teste ou reais |

#### Ambientes de requisições

##### Estes **ambientes** apenas aplicam-se à operações relacionadas com emissão e consulta de documentos.

| Ambiente | Finalidade | Validade dos documentos | Persistência dos dados |
| --- | --- | --- | --- |
| Development | Testes e integração | ❌ Sem validade fiscal/comercial | ⚠️ Podem ser apagados sem aviso |
| Production | Operações reais | ✅ Válidos perante a AGT | 🔒 Permanentes e irreversíveis |

> **Atenção:** No ambiente **Development**, os documentos emitidos não têm validade jurídica e os dados podem ser eliminados a qualquer momento sem aviso prévio. Utilize este ambiente apenas para testar a integração e tenha atenção para evitar engano, pois, todas **operações** de cada ambiente são **irreversíveis**.

---

## 3. Dados constantes

Nesta secção, listamos informações pré-definidos e necessários para diversas situações nos parâmetros/campos.

### 3.2. Pacotes de licença

A tabela segue a lista de pacotes de licença Ulemo para renovação de contas comerciantes via API.

| ID | Prazo | Preço |
| --- | --- | --- |
| 1 | Mensal | Consultar no site ou Painel Distribuidor |
| 2 | Trimestral | Consultar no site ou Painel Distribuidor |
| 3 | Semestral | Consultar no site ou Painel Distribuidor |
| 4 | Anual | Consultar no site ou Painel Distribuidor |

### 3.3. Tipos de documentos

A tabela abaixo apresenta os tipos de documentos disponíveis para emissão via integração de API.

| Tipo | Designação |
| --- | --- |
| FT | Factura |
| FR | Factura/Recibo |
| FA | Factura de Adiantamento |
| AF | Autofacturação |
| NC | Nota de Crédito |
| RC | Recibo |
| PP | Factura Pró-forma |
| OR | Orçamento |

### 3.4. Taxas & Imposto (IVA)

A tabela abaixo apresenta os impostos disponíveis para utilização nos artigos durante a emissão de documentos através da API. Cada imposto deve ser identificado pelo respectivo **código** no momento da integração.

| Código | Imposto | Taxa (%) |
| --- | --- | --- |
| ISE | Isento | 0.00 |
| NOR | IVA | 14.00 |
| INT | IVA | 7.00 |
| RED | IVA | 5.00 |

### 3.5. Isenções

A tabela abaixo apresenta os *códigos de motivo de isenção* aplicáveis a artigos sem cobrança de **IVA** durante a emissão de documentos através da API. Estes códigos devem ser utilizados sempre que o imposto do artigo estiver definido como **ISE (Isento)**.

| Código | Motivo de Isenção |
| --- | --- |
| M00 | Regime Transitório |
| M02 | Transmissão de bens e serviços não sujeita |
| M04 | IVA — Regime de Não Sujeição |
| M11 | Isento ao abrigo do Artigo 12.º, alínea b) do CIVA |
| M12 | Isento ao abrigo do Artigo 12.º, alínea c) do CIVA |
| M13 | Isento ao abrigo do Artigo 12.º, alínea d) do CIVA |
| M14 | Isento ao abrigo do Artigo 12.º, alínea e) do CIVA |
| M15 | Isento ao abrigo do Artigo 12.º, alínea f) do CIVA |
| M17 | Isento ao abrigo do Artigo 12.º, alínea h) do CIVA |
| M18 | Isento ao abrigo do Artigo 12.º, alínea i) do CIVA |
| M19 | Isento ao abrigo do Artigo 12.º, alínea j) do CIVA |
| M20 | Isento ao abrigo do Artigo 12.º, alínea k) do CIVA |
| M30 | Isento ao abrigo do Artigo 15.º, n.º 1, alínea a) do CIVA |
| M31 | Isento ao abrigo do Artigo 15.º, n.º 1, alínea b) do CIVA |
| M32 | Isento ao abrigo do Artigo 15.º, n.º 1, alínea c) do CIVA |
| M33 | Isento ao abrigo do Artigo 15.º, n.º 1, alínea d) do CIVA |
| M34 | Isento ao abrigo do Artigo 15.º, n.º 1, alínea e) do CIVA |
| M35 | Isento ao abrigo do Artigo 15.º, n.º 1, alínea f) do CIVA |
| M36 | Isento ao abrigo do Artigo 15.º, n.º 1, alínea g) do CIVA |
| M37 | Isento ao abrigo do Artigo 15.º, n.º 1, alínea h) do CIVA |
| M38 | Isento ao abrigo do Artigo 15.º, n.º 1, alínea i) do CIVA |

### 3.6. Grupos de artigo

A tabela abaixo apresenta os tipos de incidência aplicáveis aos artigos durante a emissão de documentos. Para artigos do tipo **P (Produto)**, deve ser utilizado obrigatoriamente o código **TB (Transmissão de Bens)**.

| Sigla | Tipo de Incidência |
| --- | --- |
| SE | Prestação de Serviços de Educação |
| SS | Prestação de Serviços de Saúde |
| STP | Serviços de Transporte de Passageiros |
| SR | Serviços sujeitos a Royalties |
| SIF | Intermediação Financeira / Seguros |
| SHS | Hotelaria e Similares |
| ST | Telecomunicações |
| SG | Serviço Geral (Outros) |
| TB | Transmissão de Bens |
| AS | Arrendamento e Subarrendamento |
| QT | Quotas |
| RD | Repasse de Despesas |

### 3.7. Tipos de descontos

A tabela abaixo apresenta os tipos de **descontos** suportados pela API para aplicação em artigos ou no total do documento. O valor informado deve corresponder ao tipo de desconto seleccionado.

| Tipo | Descrição |
| --- | --- |
| Percentagem | Desconto aplicado em percentagem (%) |
| Akz | Desconto aplicado em valor monetário (Kz) |

### 3.8. Motivo Nota de Crédito

Para a emissão de **Nota de Crédito (NC)**, é obrigatório informar o *motivo* da anulação ou rectificação, bem como a respectiva *causa*, descrita em texto livre conforme a necessidade da operação.

*Exemplo de causa:* Cliente efectuou a compra por engano, Foi uma falha técnica,...

| Motivo | Descrição |
| --- | --- |
| ANL | Anulação total ou parcial da factura |
| RTF | Rectificação de valores ou devolução |

### 3.9. Retenção na Fonte

A **retenção na fonte**, na emissão de documentos, segue regras estritas de validação e apenas admite os valores pré-definidos abaixo. Qualquer tentativa de envio de outras taxas será ignorada pelo sistema.

| Taxa de Retenção (%) | Descrição |
| --- | --- |
| 0.00 | Sem retenção |
| 6.5 | Retenção de IRS/IRPC aplicável a serviços |

**Regras de aplicação**

- A retenção na fonte é aplicável exclusivamente a artigos do tipo `S` (Serviço);
- O artigo deve ter o campo de retenção definido como `Sujeito` para que a retenção seja considerada no cálculo do documento;
- Caso o campo esteja definido como `NA` (Não Aplicado), a retenção não será aplicada, independentemente da **taxa informada** e do artigo se do tipo `S` (Serviço);
- O sistema não permite configuração livre de percentagens — apenas os valores definidos nesta tabela são aceites.

---

## 4. Comerciantes

Esta secção permite ao Distribuidor gerir as contas dos seus comerciantes (clientes finais). Através destes endpoints, o Distribuidor pode criar novas contas, editar informações, renovar licenças e configurar as chaves de facturação electrónica necessárias para emissão de documentos fiscais válidos.

**Importante:** As operações de criação, edição, renovação e configuração de comerciantes são sempre reais e irreversíveis, independentemente do ambiente utilizado posteriormente para emissão de documentos.

Não existe conceito de "ambiente de teste" para a gestão de comerciantes. Um comerciante criado existe no sistema e as suas licenças têm custos reais descontados da carteira do Distribuidor.

### 4.10. Criar comerciante

Cria uma nova conta de comerciante associada ao Distribuidor. O saldo da carteira do Distribuidor é debitado automaticamente. Assim que for criar comerciante, automáticamente será emitido e retornado **ID** de estabelecimento SEDE e do utilizador padrão (ADMIN) para conta criada.

**Endpoint:** `POST /comerciante/add`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros (form-data)

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| nome | String | ✅ | Nome do comerciante |
| nif | String | ✅ | NIF do comerciante (9 ou 14 dígitos) |
| tipo | String | ✅ | Empresa ou Particular |
| telefone | String | ✅ | Número de telefone (apenas operadoras angolanas com 9 dígitos) |
| email | String | ❌ | E-mail do comerciante |
| endereco | String | ❌ | Endereço do comerciante |
| dataNasc | String | ⚠️ | Obrigatório se tipo = Particular (formato yyyy-MM-dd) |
| PACOTE | Long | ✅ | ID do pacote (1=Mensal, 2=Trimestral, 3=Semestral, 4=Anual) |
| logoComerciante | File | ❌ | Logótipo do comerciante (upload) |

#### Exemplo de Requisição (cURL)

```bash
curl -X POST https://api.ulemo.ao/api/integration/v1/comerciante/add \
 -H "API-Key-Distribuidor: ulemoiba_live_abc123def456" \
 -F "nome=Empresa ABC, LDA" \
 -F "nif=5001234567" \
 -F "tipo=Empresa" \
 -F "telefone=923456789" \
 -F "email=contato@empresaabc.ao" \
 -F "endereco=Luanda, Maianga" \
 -F "PACOTE=1"
```

#### **Resposta de Sucesso**

```json
{
 "success": true,
 "message": "Comerciante registado com êxito",
 "data": {
 "id": 12345,
 "nif": "5001234567",
 "apiKeyDevelopment": "ulemoiba_test_8f3a2e1buiba1",
 "apiKeyProduction": "ulemoiba_live_7d4g5h6juiba2",
 "dataCriacao": "2026-06-12",
 "idEstabelecimentoSEDE": 67890,
 "idUserADMIN": 11111
 } }
```

#### **Resposta de Erro (Ex.: Saldo Insuficiente)**

```json
{
 "success": false,
 "message": "O saldo do Distribuidor Ulemo não é suficiente para criar Comerciante com Pacote Mensal"
 }
```

### 4.11. Editar comerciante

Actualiza os dados de um comerciante existente.

**Endpoint:** `POST /comerciante/edit`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros (form-data)

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| id | Long | ✅ | ID do comerciante |
| nome | String | ✅ | Nome do comerciante |
| nif | String | ✅ | NIF do comerciante |
| telefone | String | ✅ | Número de telefone |
| email | String | ❌ | E-mail |
| endereco | String | ❌ | Endereço |
| dataNasc | String | ⚠️ | Obrigatório se tipo = Particular |
| logoComerciante | File | ❌ | Novo logótipo |

### 4.12. Listar Comerciantes

Retorna uma lista paginada de comerciantes associados ao Distribuidor. Cada página pode conter até 100 comerciantes.

**Endpoint:** `GET /comerciante/list-comerciantes/{PAGE}`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros da URL

| Parâmetro | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| PAGE | Integer | ✅ | Número da página a consultar (inicia em 0) |

#### Exemplo de Requisição

`GET /comerciante/list-comerciantes/0`

#### Resposta

```json
{
  "success": true,
  "message": "Consulta realizada com sucesso",
  "data": {
    "page": 0,
    "total": 45,
    "comerciantes": [
      {
        "id": 12345,
        "nome": "Empresa ABC, LDA",
        "nif": "5001234567",
        "tipo": "Empresa",
        "telefone": "923456789",
        "dataCriacao": "2026-06-12"
      }
    ]
  }
}
```

### 4.13. Consultar por ID

A consulta por ID, obtém os detalhes completos de um comerciante específico.

**Endpoint:** `GET /comerciante/find-comerciante-by-id/{ID}`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros da URL

| Parâmetro | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| ID | Long | ✅ | ID do comerciante a consultar |

#### Exemplo de Requisição

`GET /comerciante/find-comerciante-by-id/12345`

#### Resposta

```json
{
  "success": true,
  "message": "Consulta realizada com sucesso",
  "data": {
    "nome": "Empresa ABC, LDA",
    "nif": "5001234567",
    "tipo": "Empresa",
    "telefone": "923456789",
    "dataCriacao": "2026-06-12",
    "logo": "logo.png",
    "chavesFE": "Pendente",
    "diasRestantesLicenca": 25
  }
}
```

### 4.14. Renovar Licença

Renova a licença (assinatura) de um comerciante. O saldo da carteira do Distribuidor é debitado automaticamente.

**Endpoint:** `POST /comerciante/renovar-licenca`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros (form-data)

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| ID_COMERCIANTE | Long | ✅ | ID do comerciante |
| PACOTE | Long | ✅ | ID do pacote (1=Mensal, 2=Trimestral, 3=Semestral, 4=Anual) |

#### **Resposta de Sucesso**

```json
{
  "success": true,
  "message": "Licença renovada com êxito"
}
```

### 4.15. Chaves F. electrónica

Configura o pare de chaves da facturação electrónica do comerciante para emissão de documentos fiscais válidos. O comerciante pode obter este par de chaves no portal **quiosque da AGT**, consulte tutoriais do Ulemo, há um vídeo que instrue a obter estas chaves.

**Endpoint:** `POST /comerciante/configurar-chaves-fe`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros (form-data)

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| ID_COMERCIANTE | Long | ✅ | ID do comerciante |
| privada | String | ✅ | Chave privada (copiada do Portal da AGT) |
| publica | String | ✅ | Chave pública (copiada do Portal da AGT) |

#### **Resposta de Sucesso**

```json
{
  "success": true,
  "message": "Chaves de facturação eletrónica configuradas com êxito"
}
```

---

## 5. Estabelecimentos

Esta secção permite ao Distribuidor gerir as filiais, lojas ou pontos de venda associados a cada comerciante. Através destes endpoints, o Distribuidor pode criar, editar, consultar ou excluir estabelecimentos.

**Importante:** Tal como na gestão de comerciantes, as operações com estabelecimentos são sempre reais.

Um estabelecimento criado existe efectivamente no sistema e não existe ambiente de teste para esta secção.

A exclusão de um estabelecimento só é permitida se o mesmo nunca tiver emitido qualquer documento.

### 5.16. Criar estabelecimento

Por padrão, Ulemo regista estabelecimento SEDE automáticamente quando comerciante é criado, caso haja mais postos (filial ou loja extra), crie e lembre-se que estabelecimentos diferentes da SEDE, devem estar préviamente cadastradas no portal da AGT.

**Endpoint:** `POST /estabelecimento/add`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros (form-data)

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| ID_COMERCIANTE | Long | ✅ | ID do comerciante |
| nome | String | ✅ | Nome do estabelecimento |
| numero | String | ✅ | Número identificador (máx. 9 caracteres) |
| provincia | String | ✅ | Província |
| municipio | String | ✅ | Município |
| endereco | String | ✅ | Endereço completo |

#### **Resposta de Sucesso**

```json
{
 "success": true,
 "message": "Estabelecimento registado com êxito!",
 "data": {
 "id": 67890,
 "dataCriacao": "2026-06-12",
 "numero": "FILIAL-001"
 } }
```

### 5.17. Editar estabelecimento

Actualiza os dados de um estabelecimento existente.

**Endpoint:** `POST /estabelecimento/edit`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros (form-data)

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| ID_COMERCIANTE | Long | ✅ | ID do comerciante |
| id | Long | ✅ | ID do estabelecimento |
| nome | String | ✅ | Nome do estabelecimento |
| numero | String | ✅ | Número identificador |
| provincia | String | ✅ | Província |
| municipio | String | ✅ | Município |
| endereco | String | ✅ | Endereço |

#### **Resposta de Sucesso**

```json
{
  "success": true,
  "message": "Estabelecimento actualizado com êxito"
}
```

### 5.18. Apagar estabelecimento

Remove um estabelecimento. A remoção só é permitida caso o estabelecimento não possua documentos emitidos.

**Endpoint:** `POST /estabelecimento/delete`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros (form-data)

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| ID_COMERCIANTE | Long | ✅ | ID do comerciante |
| id | Long | ✅ | ID do estabelecimento |

#### **Resposta de Sucesso**

```json
{
  "success": true,
  "message": "Estabelecimento apagado com êxito"
}
```

### 5.19. Listar Estabelecimentos

Lista todos os estabelecimentos associados a um comerciante.

**Endpoint:** `GET /estabelecimento/list-all-estabelecimentos-by-comerciante/{ID_COMERCIANTE}`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros da URL

| Parâmetro | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| ID_COMERCIANTE | Long | ✅ | ID do comerciante cujos estabelecimentos serão listados |

### 5.20. Consultar por ID

Consultar Estabelecimento por ID, obtém os detalhes de um estabelecimento específico.

**Endpoint:** `GET /estabelecimento/find-estabelecimento-by-id/{ID_ESTABELECIMENTO}/{ID_COMERCIANTE}`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros da URL

| Parâmetro | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| ID_ESTABELECIMENTO | Long | ✅ | ID do estabelecimento a consultar |
| ID_COMERCIANTE | Long | ✅ | ID do comerciante proprietário do estabelecimento |

---

## 6. Utilizadores

Esta secção permite ao Distribuidor gerir os utilizadores (operadores) que terão acesso à conta de cada comerciante. Através destes endpoints, o Distribuidor pode criar, editar, consultar ou excluir utilizadores com perfil de operador.

**Importante:** As operações com utilizadores são sempre reais. O utilizador administrador (ADMIN) é criado automaticamente no momento da criação do comerciante e não pode ser removido pela API.

Apenas utilizadores com perfil de operador (OPERADOR) podem ser criados, editados ou removidos. Não existe ambiente de teste para esta secção.

### 6.21. Criar utilizador

Cria um novo utilizador com perfil de OPERADOR para um comerciante.

**Endpoint:** `POST /utilizador/add`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros (form-data)

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| ID_COMERCIANTE | Long | ✅ | ID do comerciante |
| nome | String | ✅ | Nome do utilizador |
| telefone | String | ✅ | Número de telefone |
| email | String | ❌ | E-mail |
| genero | String | ❌ | Masculino, Feminino ou Indefinido |

### 6.22. Editar utilizador

Actualiza os dados de um utilizador existente.

**Endpoint:** `POST /utilizador/edit`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros (form-data)

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| id | Long | ✅ | ID do utilizador |
| ID_COMERCIANTE | Long | ✅ | ID do comerciante |
| nome | String | ✅ | Nome do utilizador |
| telefone | String | ✅ | Número de telefone |
| email | String | ❌ | E-mail |
| genero | String | ❌ | Masculino, Feminino ou Indefinido |

### 6.23. Apagar utilizador

Remove um utilizador do tipo OPERADOR. Não é permitido remover administradores e utilizadores com actividades importantes no sistema.

**Endpoint:** `POST /utilizador/delete`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros (form-data)

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| id | Long | ✅ | ID do utilizador |

### 6.24. Listar Utilizadores

Lista todos os utilizadores associados a um comerciante.

**Endpoint:** `GET /utilizador/list-all-utilizadores-by-comerciante/{ID_COMERCIANTE}`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros da URL

| Parâmetro | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| ID_COMERCIANTE | Long | ✅ | ID do comerciante cujos utilizadores serão listados |

### 6.25. Consultar por ID

Consultar Utilizador por ID, obtém os detalhes de um utilizador específico.

**Endpoint:** `GET /utilizador/find-utilizador-by-id/{ID_UTILIZADOR}/{ID_COMERCIANTE}`

#### Headers

`API-Key-Distribuidor: {chave_do_distribuidor}`

#### Parâmetros da URL

| Parâmetro | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| ID_UTILIZADOR | Long | ✅ | ID do utilizador a consultar |
| ID_COMERCIANTE | Long | ✅ | ID do comerciante associado ao utilizador |

---

## 7. Documentos

Esta secção destina-se às operações relacionadas com documentos, incluindo emissão, anulação, consulta e listagem.

Antes de executar qualquer operação, deve garantir que está a utilizar o ambiente correcto da requisição — **Produção** ou **Desenvolvimento** — de forma a evitar impacto em dados reais ou inconsistências entre ambientes.

### 7.26. Parâmetros de entrada

Abaixo encontra a descrição detalhada de cada campo utilizado nas requisições da **API Ulemo**. Esta secção foi organizada para que o **programador** compreenda rapidamente a finalidade e as regras de cada parâmetro.

#### **infoDocument** (objecto principal)

Objecto que contém todas as informações gerais do documento a ser emitido, incluindo **tipo**, **valores**, **datas** e **referência interna** se necessário.

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| tipoDoc | String | ✅ | Tipo de documento (FT, FR, AF, FA, NC, RC, PP, OR) |
| idUtilizador | Long | ✅ | ID do utilizador emissor do documento, se for ADMIN use 0 |
| ibanConta1 | String | ⚠️ | IBAN da coordenada bancária primária |
| ibanConta2 | String | ⚠️ | IBAN da coordenada bancária alternativa |
| numeroConta1 | String | ⚠️ | Nº da conta de coordenada bancária primária |
| numeroConta2 | String | ⚠️ | Nº da conta de coordenada bancária alternativa |
| titularConta1 | String | ⚠️ | Titular da coordenada bancária primária |
| titularConta2 | String | ⚠️ | Titular da coordenada bancária alternativa |
| bancoConta1 | String | ⚠️ | Nome do Banco da coordenada bancária primária (Ex.: BAI - Banco Angolano de investimentos...) |
| bancoConta2 | String | ⚠️ | Nome do Banco da coordenada bancária alternativa (Ex.: BIC - Banco de Comércio e Indústria, ...) |
| dataVencimento | Date | ⚠️ | Obrigatório para FT, PP e OR (yyyy-MM-dd) |
| numeroEstabelecimento | String | ✅ | Identificador do estabelecimento (SEDE, FILIAL-001) |
| referenciaInterna | String | ⚠️ | Referência interna do sistema |
| obs | String | ⚠️ | Observações (máx. 60 caracteres) |
| totalFinal | BigDecimal | ✅ | Total final do documento |
| descontoGlobal | BigDecimal | ⚠️ | Desconto global aplicado |
| tipoDescontoGlobal | String | ⚠️ | Percentagem ou Akz |
| retencaoFonte | BigDecimal | ⚠️ | 0 ou 6.5 (%) |
| numeroDocOrigem | String | ⚠️ | Obrigatório para NC e RC |
| motivo | String | ⚠️ | ANL ou RTF - Obrigatório para **Nota de crédito (NC)** |
| causa | String | ⚠️ | Descrição do motivo (máx. 60 caracteres) - Obrigatório para **Nota de crédito (NC)** |

#### **infoCliente** (objecto do cliente)

Objecto que contém os dados do comprador ou destinatário do documento. O sistema cria ou reutiliza clientes automaticamente com base no NIF.

#### **artigosDoc** (lista de artigos)

Lista de artigos incluídos no documento. Obrigatório para todos os documentos excepto **Recibo (RC)**.

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| nome | String | ✅ | Nome do artigo |
| codigoArtigo | String | ✅ | Código único |
| codigoImposto | String | ✅ | NOR ou ISE |
| codigoMotivoIsencao | String | ⚠️ | Obrigatório se ISE |
| retencaoFonte | String | ⚠️ | NA ou Sujeito |
| taxaImposto | BigDecimal | ✅ | 0, 7 ou 14 |
| tipo | String | ✅ | P ou S |
| grupoArtigo | String | ✅ | TB, SG, SE... |
| precoVendaUnitario | BigDecimal | ✅ | Preço unitário |
| precoCompraUnitario | BigDecimal | ✅ | Custo |
| qtd | BigDecimal | ✅ | Quantidade |
| descontoArtigo | BigDecimal | ✅ | Desconto |
| tipoDescontoArtigo | String | ✅ | Percentagem ou Akz |
| nomeCategoria | String | ✅ | Categoria |

### 7.27. Criar factura

A abordagem utilizada para a emissão de Factura (FT) aplica-se igualmente aos restantes documentos primários, seguindo a mesma estrutura e regras de validação da API.

No entanto, os documentos secundários — nomeadamente Nota de Crédito (NC) e Recibo (RC) — possuem regras adicionais, uma vez que dependem de um documento de origem e podem exigir parâmetros específicos associados à operação.

#### Endpoint

```
POST /documento/emitir
```

#### Headers

| Header | Valor |
| --- | --- |
| API-Key-Distribuidor | {Chave_Distribuidor_ulemo_aqui} |
| API-Key-Comerciante | {Chave_Comerciante_aqui} |
| Environment | Production / Development |
| Content-Type | application/json |

#### Parâmetros de entrada

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| infoDocument.tipoDoc | String | ✅ | Deve ser FT |
| infoDocument.dataVencimento | Date | ✅ | yyyy-MM-dd |
| infoDocument.numeroEstabelecimento | String | ✅ | Ex: SEDE |
| infoDocument.totalFinal | BigDecimal | ✅ | Total da factura |
| infoDocument.descontoGlobal | BigDecimal | ✅ | Desconto global |
| infoDocument.tipoDescontoGlobal | String | ✅ | Percentagem ou Akz |
| infoDocument.retencaoFonte | BigDecimal | ✅ | 0 ou 6.5 |
| infoCliente.nome | String | ✅ | Nome do cliente |
| infoCliente.nif | String | ✅ | NIF do cliente |
| infoCliente.pais | String | ✅ | País do cliente |
| artigosDoc | Array | ✅ | Lista de artigos |

#### Exemplo prático

```
{
"infoDocument":
 { "tipoDoc": "FT",
 "dataVencimento": "2026-06-30".,
 "numeroEstabelecimento": "SEDE",
 "referenciaInterna": "FT-2026-001",
 "obs": "Venda de equipamentos informáticos",
 "totalFinal": 162450.00,
 "descontoGlobal": 5,
 "tipoDescontoGlobal": "Percentagem",
 "retencaoFonte": 0 },
 "infoCliente":
 {
 "nome": "Empresa ABC, LDA",
 "pais": "Angola",
 "nif": "5001234567",
 "tipo": "Empresa",
 "endereco": "Luanda, Maianga, Rua X",
 "telefone": "+244923456789",
 "email": "contato@empresaabc.ao" },
 "artigosDoc": [
 {
 "nome": "Computador Desktop",
 "codigoArtigo": "PC-001",
 "codigoImposto": "NOR", "taxaImposto": 14,
 "grupoArtigo": "TB",
 "precoVendaUnitario": 150000.00,
 "qtd": 1,
 "descontoArtigo": 0,
 "tipoDescontoArtigo": "Akz",
 "nomeCategoria": "Informática",
 "retencaoFonte": "NA"
 } ] }
```

#### Resposta de sucesso

```json
{
"success": true,
 "message": "Factura emitida com sucesso",
 "data": {
 "id": 12345,
 "numero": "FT/00123",
 "estado_fe_Agt": "SUBMETIDO",
 "environment": "Production"
 } }
```

#### Resposta de erro

```json
{
 "success": false,
 "error": "Estabelecimento com número 'SEDE' não encontrado",
 "field": "infoDocument.numeroEstabelecimento"
 }
```

### 7.28. Criar Recibo

O Recibo é utilizado para comprovar o pagamento de uma Factura. Pode ser parcial (pagamento de parte do valor) ou total (liquidação completa da dívida).

#### Endpoint

```
POST /documento/emitir-recibo
```

#### Headers

| Header | Valor |
| --- | --- |
| API-Key-Distribuidor | {Chave_Distribuidor_aqui} |
| API-Key-Comerciante | {Chave_Comerciante_aqui} |
| Environment | Production (apenas) |
| Content-Type | application/json |

#### Parâmetros de entrada

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| infoDocument.tipoDoc | String | ✅ | Deve ser RC |
| infoDocument.numeroEstabelecimento | String | ✅ | Número do estabelecimento |
| infoDocument.totalFinal | BigDecimal | ✅ | Valor a pagar neste recibo |
| infoDocument.numeroDocOrigem | String | ✅ | Número da Factura original (ex: FT/00123) |
| infoDocument.valorEntregueDinheiro | BigDecimal | ⚠️ | Valor pago em dinheiro |
| infoDocument.valorEntregueBanco | BigDecimal | ⚠️ | Valor pago por transferência |
| infoCliente.nif | String | ✅ | NIF do cliente (mesmo da Factura) |
| artigosDoc | Array | ❌ | Deve ser null |

> **Nota:** A soma de **valorEntregueDinheiro** + **valorEntregueBanco** deve ser igual ou superior ao totalFinal.

#### Exemplo prático

```json
{ "infoDocument": {
"tipoDoc": "RC",
"numeroEstabelecimento": "SEDE",
"referenciaInterna": "RC-2026-001",
"obs": "Pagamento parcial da factura",
"totalFinal": 50000.00,
"numeroDocOrigem": "FT/00123",
"valorEntregueDinheiro": 30000.00,
"valorEntregueBanco": 20000.00
},
"infoCliente": { "nif": "5001234567" },
"artigosDoc": null }
```

#### Resposta de sucesso

```json
{
"success": true,
"message": "Recibo emitido com sucesso",
"data": {
"id": 12346,
"numero": "RC/00123",
"estado_fe_Agt": "NA",
"environment": "Production",
"link_pdf_A4": "...",
"link_pdf_58mm": "...",
"link_pdf_80mm": "..."
} }
```

#### Resposta de erro

```json
{
"success": false,
"error": "A Factura que pretende emitir RECIBO já foi Anulada",
"field": "infoDocument.numeroDocOrigem"
}
```

### 7.29. Criar Nota de Crédito

A Nota de Crédito é utilizada para anular ou rectificar uma Factura já emitida. Pode ser total (anulação completa) ou parcial (devolução de alguns artigos).

#### Endpoint

```
POST /documento/emitir-nota-credito
```

#### Headers

| Header | Valor |
| --- | --- |
| API-Key-Distribuidor | {Chave_Distribuidor_aqui} |
| API-Key-Comerciante | {chave_Comerciante_aqui} |
| Environment | Production (apenas) |
| Content-Type | application/json |

#### Parâmetros de entrada

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| infoDocument.tipoDoc | String | ✅ | Deve ser NC |
| infoDocument.numeroEstabelecimento | String | ✅ | Número do estabelecimento |
| infoDocument.totalFinal | BigDecimal | ✅ | Valor a ser creditado |
| infoDocument.numeroDocOrigem | String | ✅ | Número da Factura original |
| infoDocument.motivo | String | ✅ | ANL (anulação) ou RTF (rectificação) |
| infoDocument.causa | String | ✅ | Descrição do motivo (máx. 60 caracteres) |
| infoCliente.nif | String | ✅ | NIF do cliente (mesmo da Factura) |
| artigosDoc | Array | ✅ | Lista de artigos a creditar |

#### Exemplo prático

```json
{
"infoDocument":
{
"tipoDoc": "NC",
"numeroEstabelecimento": "SEDE",
"referenciaInterna": "NC-2026-001",
"obs": "Devolução de produto com defeito",
"totalFinal": 500.00,
"numeroDocOrigem": "FT/00123",
"motivo": "ANL",
"causa": "Cliente enganou-se na compra"
},
"infoCliente": { "nif": "5001234567" },
"artigosDoc": [
{
"nome": "Mouse Óptico",
"codigoArtigo": "MOU-001",
"codigoImposto": "NOR",
"taxaImposto": 14,
"grupoArtigo": "TB",
"precoVendaUnitario": 500.00,
"qtd": 1,
"nomeCategoria": "Informática"
} ] }
```

#### Regras importantes

- O valor da **Nota de Crédito** não pode exceder o valor pendente da factura original.
- A quantidade devolvida não pode ser superior à quantidade original.
- Se devolver todos os artigos, a Nota de Crédito é considerada "Completa".
- A **Nota de Crédito** só pode ser emitida para *Facturas* (FT), *Facturas/Recibo* (FR) ou *Autofacturas* (AF).

#### Resposta de sucesso

```json
{
"success": true,
"message": "Nota de Crédito nº #NC/00123 emitida com sucesso.",
"data": {
"id": 12347,
"numero": "NC/00123",
"tipoNota": "Parcial",
"motivo": "ANL",
"estado_fe_Agt": "SUBMETIDO",
"environment": "Production",
"link_pdf_A4": "..."
} }
```

#### Resposta de erro

```json
{
"success": false,
"error": "O valor total da Nota de Crédito não deve ser superior ao valor pendente do documento original (Kz 1000.00)",
"field": "infoDocument.totalFinal"
}
```

### 7.30. Consultar por ID

Este **endpoint** permite obter os detalhes de um *documento específico*, identificado pelo seu **ID** e **estabelecimento** onde foi emitido.

A resposta inclui informações do documento, incluindo dados do cliente, valores calculados, estado fiscal e links para download do PDF.

#### Endpoint

```
GET /find-by-estabeleciment-and-id/{STB}/{ID}
```

#### Parâmetros de caminho (Path Variables)

| Parâmetro | Tipo | Descrição | Exemplo |
| --- | --- | --- | --- |
| STB | String | Número do estabelecimento onde o documento foi emitido | SEDE, 001 |
| ID | Long | ID interno do documento | 12345 |

#### Headers

| Header | Valor | Obrigatório |
| --- | --- | --- |
| API-Key-Distribuidor | {Chave_Distribuidor_aqui} | ✅ |
| API-Key-Comerciante | {Chave_Comerciante_aqui} | ✅ |
| Environment | Production / Development | ✅ |

#### Funcionamento

O sistema valida se o documento existe e se pertence ao estabelecimento informado. Caso exista, retorna os dados do documento, incluindo cliente, valores, impostos e links de PDF.

O ID utilizado no link do PDF é automaticamente codificado por razões de segurança.

#### Exemplo de requisição

```
GET /api/integration/v1/documento/find-by-estabeleciment-and-id/SEDE/12345
```

#### Resposta de sucesso (Factura)

```json
{
 "success": true,
 "message": "Documento obtido com sucesso",
 "data": {
 "id": 12345,
 "codigo": "FAC-2026-00123",
 "numero": "FT/00123",
 "tipo": "FT",
 "data": "24/05/2026",
 "hora": "10:15",
 "dataVencimento": "30/06/2026",
 "nomeCliente": "Empresa ABC, LDA",
 "nifCliente": "5001234567",
 "enderecoCliente": "Luanda, Maianga, Rua X",
 "telefoneCliente": "+244923456789",
 "emailCliente": "contato@empresaabc.ao",
 "valorSubtotal": 150000.00,
 "valorTotal": 162450.00,
 "valorTotalImposto": 19950.00,
 "valorTotalDesconto": 7500.00,
 "descontoGlobal": 5,
 "tipoDescontoGlobal": "Percentagem",
 "retencaoFonte": 0,
 "estado_fe_Agt": "SUBMETIDO",
 "environment": "Production",
 "link_pdf_A4": "https://api.ulemo.ao/api/integration/pre-viwer-doc-api/facturacao/documento/Production/XYZ791/4/abc123"
 } }
```

#### Resposta de sucesso (Nota de Crédito)

```json
{
 "success": true,
 "message": "Documento obtido com sucesso",
 "data": {
 "id": 12347,
 "codigo": "NCD-2026-00123",
 "numero": "NC/00123", "tipo": "NC",
 "data": "24/05/2026",
 "hora": "15:30",
 "nomeCliente": "Empresa ABC, LDA",
 "nifCliente": "5001234567",
 "valorTotal": 500.00,
 "motivo": "ANL",
 "estado_fe_Agt": "SUBMETIDO",
 "numeroDocPai": "FT/00123",
 "environment": "Production",
 "link_pdf_A4": "https://api.ulemo.ao/api/integration/pre-viwer-doc-api/facturacao/documento/Production/XYZ789/4/abc123"
 } }
```

#### Resposta de sucesso (Recibo)

```json
{
 "success": true,
 "message": "Documento obtido com sucesso",
 "data": {
 "id": 12346,
 "codigo": "RCB-2026-00123",
 "numero": "RC/00123",
 "tipo": "RC",
 "data": "24/05/2026",
 "hora": "14:20",
 "nomeCliente": "Empresa ABC, LDA",
 "nifCliente": "5001234567",
 "valorTotal": 50000.00,
 "valorEntregueDinheiro": 30000.00,
 "valorEntregueBanco": 20000.00,
 "numeroDocPai": "FT/00123",
 "estado_fe_Agt": "NA",
 "environment": "Production",
 "link_pdf_A4": "https://api.ulemo.ao/api/integration/pre-viwer-doc-api/facturacao/documento/Production/XYZ790/4/abc123"
 } }
```

#### Resposta quando documento não encontrado

```json
{
 "success": false,
 "message": "Documento não encontrado"
 }
```

#### Resposta de erro (validação)

```json
{
 "success": false,
 "error": "Estabelecimento com número 'SEDE' não encontrado",
 "field": "infoDocument.numeroEstabelecimento"
 }
```

### 7.31. Listagem por Posto

Este **endpoint** permite listar documentos emitidos num determinado estabelecimento, organizados por grupo, com suporte a paginação. Os resultados são ordenados do mais recente para o mais antigo, garantindo acesso rápido aos documentos mais recentes.

É possível consultar documentos *fiscais* **(FT, FR, FA, AF, NC, RC)** ou documentos de *propostas* **(PP, OR),** conforme o grupo seleccionado.

#### Endpoint

```
GET /list-by-estabeleciment-and-group/{STB}/{group}/{PAGE}
```

#### Parâmetros de caminho (Path Variables)

| Parâmetro | Tipo | Descrição | Exemplo |
| --- | --- | --- | --- |
| STB | String | Número do estabelecimento registado no sistema | SEDE, 001, FILIAL-A |
| group | String | Grupo de documentos a listar (**fiscais** ou **propostas**) | fiscais |
| PAGE | Integer | Número da página (começa em 0). **Máx. 50** documentos por página | 0, 1, 2 |

#### Headers

| Header | Valor | Obrigatório |
| --- | --- | --- |
| API-Key-Distribuidor | {Chave_Distribuidor_ulemo_aqui} | ✅ |
| API-Key-Comerciante | {Chave_Comerciante_aqui} | ✅ |
| Environment | Production / Development | ✅ |

#### Funcionamento

Os documentos são ordenados do mais recente para o mais antigo (por ID decrescente). Cada página retorna no máximo 50 documentos. Se não existirem dados para a página solicitada, será retornada uma lista vazia.

O grupo **fiscais** inclui: FT, FR, FA, AF, NC, RC. O grupo **propostas** inclui: PP, OR.

#### Exemplo de requisição

```
GET /api/integration/v1/documento/list-by-estabeleciment-and-group/SEDE/fiscais/0
```

#### Resposta de sucesso

```json
{
 "success": true,
 "message": "Documentos listados com sucesso",
 "data": {
 "page": 0, "total": 3,
 "documentos": [
 {
 "id": 12347,
 "numero": "NC/00123",
 "data": "24/05/2026",
 "hora": "15:30",
 "tipo": "NC",
 "nomeCliente": "Empresa ABC, LDA",
 "nifCliente": "5001234567",
 "valorTotal": 500.00,
 "estado_fe_Agt": "SUBMETIDO",
 "link_pdf_A4": "https://api.ulemo.ao/api/integration/pre-viwer-doc-api/facturacao/documento/Production/XYZ789/4/abc123"
 } ], "environment": "Production" } }
```

#### Resposta de erro

```json
{
 "success": false,
 "error": "O grupo deve ser 'fiscais' ou 'propostas'",
 "field": "group"
 }
```

---

## 8. Erros & soluções

Esta secção descreve os principais códigos de erro da API, as suas causas e soluções recomendadas.

---

### **Erros de Autenticação e Cabeçalho**

| Código | Campo | Mensagem | Solução |
| --- | --- | --- | --- |
| HEADER_API_KEY_DISTRIBUIDOR_OBRIGATORIO | API-Key-Distribuidor | O header 'API-Key-Distribuidor' é obrigatório. | Adicione o header API-Key-Distribuidor com a chave fornecida pela Ulemo |
| HEADER_API_KEY_COMERCIANTE_OBRIGATORIO | API-Key-Comerciante | O header 'API-Key-Comerciante' é obrigatório. | Adicione o header API-Key-Comerciante com a chave do comerciante |
| HEADER_ENVIRONMENT_OBRIGATORIO | Environment | O header 'Environment' é obrigatório. | Adicione Environment: Production ou Development |
| HEADER_ENVIRONMENT_INVALIDO | Environment | O valor deve ser 'Production' ou 'Development'. | Corrija o valor do header Environment |
| ENVIRONMENT_INVALIDA | ENVIRONMENT | Ambiente inválido, verifique o HEADER da sua requisição | Utilize apenas Production ou Development |
| ENVIRONMENT_OR_API_key_NULA | Environment/API-Key | Ambiente ou Chaves de API não informados | Adicione todos os headers obrigatórios |

---

### **Erros de Chaves de API**

| Código | Campo | Mensagem | Solução |
| --- | --- | --- | --- |
| API_KEY_INVALIDA | API-Key | As chaves de API informadas são inválidas ou não coincidem. | Verifique se a chave do Distribuidor e do Comerciante estão correctas e se o comerciante pertence ao distribuidor |
| COMERCIANTE_SUSPENSO | COMERCIANTE_SUSPENSO | Conta comerciante suspensa, por favor contacte o seu Distribuidor Ulemo | O Distribuidor deve regularizar a situação do comerciante |

---

### **Erros de Licença e Subscrição**

| Código | Campo | Mensagem | Solução |
| --- | --- | --- | --- |
| LICENCA_NAO_ENCONTRADA | ASSINATURA | Nenhuma licença/subscrição Ulemo PAGA foi encontrada para o seu comerciante. | O Distribuidor deve adquirir um plano para o comerciante |
| SUBSCRICAO_EXPIRADA | licenca | A sua subscrição Ulemo expirou há X dias. | O Distribuidor deve renovar a licença do comerciante |
| CHAVES_FACTURAÇÃO_ELECTRÓNICA_NÃO_CONFIGURADAS | CHAVES_FACTURAÇÃO_ELECTRÓNICA | Ainda não configurou as suas Chaves de Facturação Electrónica no Ulemo | Configure as chaves via endpoint /comerciante/configurar-chaves-fe |
| CHAVES_ASSINATURA_INACTIVAS | chaveAssinatura | As Chaves de Facturação Electrónica estão [estado] no Ulemo. | Active as chaves ou verifique se foram configuradas correctamente |
