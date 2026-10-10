# Documentação de Integração da API de Pagamento PayPay AO

> Fonte: <https://portal.paypayafrica.com/dist/guide/apidoc_pt.html>
> Versões noutros idiomas: [CN](https://portal.paypayafrica.com/dist/guide/apidoc.html) · [EN](https://portal.paypayafrica.com/dist/guide/apidoc_en.html) · [PT](https://portal.paypayafrica.com/dist/guide/apidoc_pt.html)

Este documento foi desenvolvido para ajudá-lo a integrar com o pagamento PayPay AO, permitindo que os usuários concluam pagamentos de forma conveniente e rápida.

## Índice

1. Início
2. Informações Básicas da Requisição
3. Descrição da API
   - 3.1 Criar Pedido de Pagamento Instantâneo
   - 3.2 Criar Pedido MULTICAIXA Express
   - 3.3 Criar Pedido MULTICAIXA Reference
   - 3.4 Iniciar Reembolso
   - 3.5 Fechar Pedido
   - 3.6 Consultar Pedido
   - 3.7 Transferir para Cartão Bancário
   - 3.8 Transferir para Conta PayPay
   - 3.9 Criar Pedido KWiK QrCode
   - 3.10 Pedido de Pagamento por Agente
   - 3.11 Levantamento por Agente
4. Notificações Assíncronas

---

## 1. Início

### 1.1 Fase de Preparação

1. Você precisa de uma conta empresarial PayPay. Você pode registrar-se em nosso aplicativo PayPay e concluir a verificação empresarial.
2. Você precisa entrar em contato conosco, e nossa equipe comercial irá guiá-lo e concluir a assinatura do contrato comercial.
3. A equipe comercial fornecerá a você o **Endereço da API**, **Partner ID** e **Sale Product Code**. Estas informações serão utilizadas durante o processo de integração.

Como desenvolvedor, você pode continuar lendo este documento e até concluir o desenvolvimento do código, aguardando o fornecimento destes dados. Após recebê-los, basta configurá-los.

### 1.2 Processo de Pagamento

Vamos primeiro ver o processo geral de pagamento PayPay para ter uma compreensão aproximada.

#### 1.2.1 Processo de Ativação do PayPay pelo Aplicativo Móvel

![Diagrama de Sequência 1](https://portal.paypayafrica.com/dist/guide/img/image-20220126040638967.png)

1. Usuário faz um pedido e seleciona pagamento via PayPay
   - 1.1 O servidor do parceiro salva o pedido, chama o servidor PayPay através da API e cria um pedido de pagamento
   - 1.2 O servidor PayPay retorna o número do pedido PayPay, URL, token e outras informações
   - 1.3 Retorna as informações para o aplicativo do comerciante ou página web móvel
2. O aplicativo do comerciante abre o aplicativo PayPay através do URL schema (abrir a URL retornada através do navegador do sistema também pode abrir o aplicativo PayPay)
   - 2.1 Usuário conclui o pagamento no aplicativo PayPay
   - 2.2 Após a conclusão do pagamento, retorna ao aplicativo do comerciante ou página web móvel
3. Após o pagamento do usuário, o servidor PayPay notificará seu servidor de forma assíncrona
   - 3.1 Exibe o status do pedido
4. Para pedidos que não receberam notificação assíncrona por um longo tempo, você pode consultar proativamente o status do pedido através da interface de consulta
   - 4.1 Retorna o resultado do pagamento
5. Altera o status do pedido

#### 1.2.2 Pagamento por Escaneamento de Código QR

![Diagrama de Sequência 2](https://portal.paypayafrica.com/dist/guide/img/image-20220126040857772.png)

1. Usuário faz um pedido e seleciona pagamento via PayPay
   - 1.1 O servidor do parceiro salva o pedido, chama o servidor PayPay através da API e cria um pedido de pagamento
   - 1.2 O servidor PayPay retorna o número do pedido PayPay, URL, token e outras informações
   - 1.3 Retorna as informações para o aplicativo
2. O aplicativo do comerciante gera um código QR a partir da URL e solicita ao usuário que escaneie usando o PayPay (você pode usar IA ou Google para descobrir quais bibliotecas na sua linguagem de programação podem gerar códigos QR a partir de texto)
3. Usuário abre o aplicativo PayPay e escaneia o código QR
4. Usuário conclui o pagamento no PayPay
5. Após o pagamento do usuário, o servidor PayPay enviará as informações do pedido e o status de forma assíncrona para seu servidor
   - 5.1 Exibe o status do pedido
6. Para pedidos que não receberam notificação assíncrona por um longo tempo, você pode consultar proativamente o status do pedido através da interface de consulta
   - 6.1 Retorna o resultado do pagamento
7. Altera o status do pedido

Se seu aplicativo estiver no celular do usuário, você deve escolher naturalmente o primeiro método, pois os usuários não podem escanear a tela do próprio celular.

Se seu aplicativo estiver em TV, computador ou tela de caixa registradora, você deve escolher o segundo método.

Depois, na integração dos canais REF e MUL, também haverá algumas diferenças, mas o processo geral é semelhante. A experiência do usuário precisa ser otimizada separadamente.

Se um pedido não for pago por um longo período, ele será fechado e seu servidor será notificado de forma assíncrona.

Se vocês integraram a função de saque, o processo também é semelhante. Primeiro crie um pedido através da API. Após a criação do pedido, ele será processado de forma assíncrona pelo PayPay. Independentemente de sucesso ou falha, o resultado do processamento será notificado de forma assíncrona para seu servidor. Vocês também podem consultar proativamente o status do pedido através da interface de consulta.

---

## 2. Informações Básicas da Requisição

### 2.1 Geração de Chaves

Antes da integração, você precisa ter uma compreensão básica de RSA, pois seu servidor precisa criptografar os dados relevantes usando RSA antes de fazer requisições à API PayPay.

As chaves pública e privada usadas para criptografia RSA precisam ser geradas por você. A chave privada deve ser mantida por você; por favor, não a entregue a ninguém, incluindo funcionários do PayPay. A chave pública precisa ser enviada por você para o site do PayPay.

Exigimos que a chave seja de **1024 bits**.

1. Fornecemos uma ferramenta para ajudá-lo a gerá-las (a "Ferramenta de Teste de Integração RSA (1024 bits)", disponível na página original). Esta ferramenta também pode ajudá-lo com depuração básica.
2. Ou você pode usar comandos `openssl` para gerar as chaves:

   ```bash
   openssl genrsa -out private.key 1024
   openssl rsa -in private.key -pubout -out public.key
   ```

3. Ou você pode encontrar outras ferramentas para gerar.

### 2.2 Upload da Chave Pública

Precisamos que você envie a chave pública que você gerou. Por favor, faça login com sua conta empresarial e envie.

![Captura de Tela](https://portal.paypayafrica.com/dist/guide/img/image-20220126043413885.png)

1. Baixe a chave pública oficial do PayPay. Por favor, não confunda com sua própria chave pública. Esta chave pública será usada na função de notificação assíncrona.
2. Faça o upload aqui. Por favor, remova os cabeçalhos, rodapés e espaços extras da chave pública conforme indicado.
3. Entre na interface de upload da chave pública através do portal.

### 2.3 Corpo da Requisição

Vamos primeiro ver como é o corpo básico de uma requisição enviada para a API PayPay.

```json
{
    "charset": "UTF-8",
    "biz_content": "GkTO5wRUFTij4GPniUQChP...",
    "partner_id": "200001835716",
    "service": "instant_trade",
    "request_no": "5298b3d713b6430c95729f9e54c967dc",
    "format": "JSON",
    "sign": "N6EPWFhOOiqP7PfgEWgffszU6SCM...",
    "language": "pt",
    "sign_type": "RSA",
    "version": "1.0",
    "timestamp": "2022-01-20 19:44:51"
}
```

| Parâmetro    | Descrição                                                                                                             |
| ------------ | --------------------------------------------------------------------------------------------------------------------- |
| `charset`    | Valor fixo: `UTF-8`                                                                                                   |
| `partner_id` | Partner ID da sua conta PayPay                                                                                        |
| `service`    | Nome da API, apresentado em detalhes mais adiante                                                                     |
| `request_no` | Número da requisição, usado principalmente para investigação de logs, não é muito útil, pode ser uma string aleatória |
| `format`     | Valor fixo: `JSON`                                                                                                    |
| `language`   | Idioma a ser usado, mensagens de erro serão exibidas no idioma correspondente: `pt` para português, `en` para inglês  |
| `sign_type`  | Valor fixo: `RSA`                                                                                                     |
| `version`    | Valor fixo: `1.0`                                                                                                     |
| `timestamp`  | Hora da requisição, siga o formato do exemplo. **O fuso horário deve ser o de Angola, GMT+1**                         |

A seguir, serão apresentados em detalhes os parâmetros **biz_content** e **sign**.

### 2.4 Parâmetro `biz_content`

**biz_content** é o parâmetro de negócio. É uma string codificada em base64 obtida ao converter o objeto de negócio em uma string JSON e, em seguida, criptografá-la com a chave privada RSA.

Os membros do objeto de negócio para cada interface são diferentes e serão apresentados em detalhes mais adiante. Você só precisa preencher de acordo com os requisitos da documentação.

Atenção também: exigimos o uso de criptografia com a **chave privada** RSA. E a string JSON será relativamente longa, geralmente sendo necessário criptografar em segmentos.

Aqui fornecemos alguns códigos de exemplo para sua referência.

**Java**

```java
public static String encryptByPrivateKey(String oriText, String privateKey) throws RSAException {
    try {
        byte[] keyBytes = Base64.getDecoder().decode(privateKey);
        PKCS8EncodedKeySpec pkcs8EncodedKeySpec = new PKCS8EncodedKeySpec(keyBytes);
        KeyFactory keyFactory = KeyFactory.getInstance("RSA");
        PrivateKey pk = keyFactory.generatePrivate(pkcs8EncodedKeySpec);
        int keyLen = ((RSAPrivateCrtKeyImpl) pk).getModulus().toString(2).length();
        Cipher cipher = Cipher.getInstance(keyFactory.getAlgorithm());
        cipher.init(Cipher.ENCRYPT_MODE, pk);
        byte[] oriBytes = oriText.getBytes(StandardCharsets.UTF_8);
        int inputLen = oriBytes.length;
        int m = keyLen / 8 - 11;
        int initStreamLen = (inputLen % m == 0 ? inputLen / m : (inputLen / m) + 1) * (keyLen / 8);
        ByteArrayOutputStream out = new ByteArrayOutputStream(initStreamLen);
        int offSet = 0;
        byte[] cache;
        int i = 0;
        while (inputLen - offSet > 0) {
            if (inputLen - offSet > m) {
                cache = cipher.doFinal(oriBytes, offSet, m);
            } else {
                cache = cipher.doFinal(oriBytes, offSet, inputLen - offSet);
            }
            out.write(cache, 0, cache.length);
            i++;
            offSet = i * m;
        }
        byte[] encryptedData = out.toByteArray();
        out.close();
        return Base64.getEncoder().encodeToString(encryptedData);
    } catch (Exception exception) {
        throw new RSAException("RSA Error", exception);
    }
}
```

**PHP**

```php
<?php

function encryptByPrivateKey($oriText, $privateKey) {
    if (strpos($privateKey, '-----BEGIN PRIVATE KEY-----') === false) {
        $privateKey = "-----BEGIN PRIVATE KEY-----\n" .
                      wordwrap($privateKey, 64, "\n", true) .
                      "\n-----END PRIVATE KEY-----";
    }

    $pkeyResource = openssl_pkey_get_private($privateKey);
    if (!$pkeyResource) {
        throw new Exception("Invalid private key");
    }

    // 2. Obter comprimento da chave (bits) e calcular tamanho do segmento
    $details = openssl_pkey_get_details($pkeyResource);
    $keyLen = $details['bits'];
    $m = $keyLen / 8 - 11;

    $oriBytes = urlencode($oriText);
    $oriBytes = json_decode('"'.$oriText.'"');
    $inputLen = strlen($oriText);

    $encryptedData = '';
    $offSet = 0;
    while ($inputLen - $offSet > 0) {
        $chunk = substr($oriText, $offSet, $m);
        $encryptedChunk = '';
        $success = openssl_private_encrypt($chunk, $encryptedChunk, $pkeyResource, OPENSSL_PKCS1_PADDING);
        if (!$success) {
            throw new Exception("Encryption failed");
        }

        $encryptedData .= $encryptedChunk;
        $offSet += $m;
    }
    return base64_encode($encryptedData);
}
```

**Node.js**

```js
const crypto = require('crypto');
function encryptByPrivateKey(oriText, privateKey) {
    try {
        let pemKey = privateKey;
        if (!privateKey.includes('-----BEGIN PRIVATE KEY-----')) {
            pemKey = `-----BEGIN PRIVATE KEY-----\n${privateKey.match(/.{1,64}/g).join('\n')}\n-----END PRIVATE KEY-----`;
        }
        const keyDetails = crypto.createPrivateKey(pemKey).export({ type: 'pkcs8', format: 'pem' });
        const keyObject = crypto.createPrivateKey(pemKey);
        const keyLen = keyObject.asymmetricKeyDetails.modulusLength;
        const m = keyLen / 8 - 11;
        const oriBytes = Buffer.from(oriText, 'utf8');
        const inputLen = oriBytes.length;
        const chunks = [];
        let offSet = 0;
        while (inputLen - offSet > 0) {
            const end = Math.min(inputLen, offSet + m);
            const chunk = oriBytes.slice(offSet, end);
            const encryptedChunk = crypto.privateEncrypt({
                key: pemKey,
                padding: crypto.constants.RSA_PKCS1_PADDING
            }, chunk);
            chunks.push(encryptedChunk);
            offSet += m;
        }
        const encryptedData = Buffer.concat(chunks);
        return encryptedData.toString('base64');
    } catch (error) {
        throw new Error("RSA Error: " + error.message);
    }
}
```

**Python**

```python
import base64
from Crypto.PublicKey import RSA
from Crypto.Cipher import PKCS1_v1_5

def encrypt_by_private_key(ori_text, private_key_str):
    try:
        if not private_key_str.startswith("-----BEGIN PRIVATE KEY-----"):
            private_key_str = f"-----BEGIN PRIVATE KEY-----\n{private_key_str}\n-----END PRIVATE KEY-----"
        key = RSA.import_key(private_key_str)
        key_len = key.size_in_bits()
        m = key_len // 8 - 11
        ori_bytes = ori_text.encode('utf-8')
        input_len = len(ori_bytes)
        encrypted_data = bytearray()
        off_set = 0
        cipher = PKCS1_v1_5.new(key)

        while input_len - off_set > 0:
            chunk = ori_bytes[off_set : off_set + m]
            from Crypto.Util.number import bytes_to_long, long_to_bytes
            pad_len = (key_len // 8) - len(chunk) - 3
            padded_chunk = b'\x00\x01' + (b'\xff' * pad_len) + b'\x00' + chunk
            c_long = pow(bytes_to_long(padded_chunk), key.d, key.n)
            encrypted_chunk = long_to_bytes(c_long, key_len // 8)
            encrypted_data.extend(encrypted_chunk)
            off_set += m
        return base64.b64encode(encrypted_data).decode('utf-8')
    except Exception as e:
        raise Exception(f"RSA Error: {str(e)}")
```

### 2.5 Parâmetro `sign`

O parâmetro `sign` é usado principalmente para evitar que atacantes mal-intencionados falsifiquem dados durante a transmissão de rede.

Depois de coletar todos os parâmetros, você pode calcular a assinatura. Primeiro gere o texto original da assinatura, depois use sua chave privada para calcular a assinatura e coloque o resultado no parâmetro `sign`. Após receber sua requisição, o servidor PayPay só aceitará a requisição se passar na verificação de assinatura.

#### 2.5.1 Gerar Texto Original da Assinatura

Colete todos os parâmetros, ordene-os pelo código ASCII dos nomes dos parâmetros do menor para o maior, exclua parâmetros com valores vazios, exclua os dois parâmetros `sign` e `sign_type`, depois gere uma string no formato `key1=value1&key2=value2`. Esta é a string original da assinatura.

**Java**

```java
public static String formatSignOriText(Map<String, String> myMap) {
    Map<String, String> result = new HashMap<>(myMap.size());
    String key;
    String value;
    for (Map.Entry<String, String> item : myMap.entrySet()) {
        key = item.getKey();
        value = item.getValue();
        if (value != null && !value.isEmpty() && !key.equalsIgnoreCase("sign") && !key.equalsIgnoreCase("sign_type")) {
            result.put(key, value);
        }
    }

    List<String> keys = new ArrayList<>(result.keySet());
    Collections.sort(keys);
    StringBuilder strBuilder = new StringBuilder(2000);
    int i = 0;
    for(int size = keys.size(); i < size; ++i) {
        key = keys.get(i);
        value = result.get(key);
        if (i == keys.size() - 1) {
            strBuilder.append(key).append('=').append(value);
        } else {
            strBuilder.append(key).append('=').append(value).append('&');
        }
    }
    return strBuilder.toString();
}
```

**PHP**

```php
function formatSignOriText(array $myMap)
{
    $result = [];
    foreach ($myMap as $key => $value) {
        if (
            $value !== null &&
            $value !== '' &&
            strcasecmp($key, 'sign') !== 0 &&
            strcasecmp($key, 'sign_type') !== 0
        ) {
            $result[$key] = $value;
        }
    }
    ksort($result);
    $params = [];
    foreach ($result as $k => $v) {
        $params[] = $k . '=' . $v;
    }
    return implode('&', $params);
}
```

**Node.js**

```js
function formatSignOriText(myMap) {
    const result = {};
    for (const key in myMap) {
        if (myMap.hasOwnProperty(key)) {
            const value = myMap[key];
            if (
                value != null &&
                value !== '' &&
                key.toLowerCase() !== 'sign' &&
                key.toLowerCase() !== 'sign_type'
            ) {
                result[key] = value;
            }
        }
    }
    const sortedKeys = Object.keys(result).sort();
    const parts = [];
    for (const key of sortedKeys) {
        parts.push(`${key}=${result[key]}`);
    }
    return parts.join('&');
}
```

**Python**

```python
def format_sign_ori_text(my_map: dict) -> str:
    result = {}
    for key, value in my_map.items():
        if (value is not None and value != '' and
                key.lower() not in ['sign', 'sign_type']):
            result[key] = value
    sorted_keys = sorted(result.keys())
    parts = []
    for key in sorted_keys:
        parts.append(f"{key}={result[key]}")
    return '&'.join(parts)
```

Usando o JSON acima como exemplo, o texto original da assinatura que você obterá deve ser assim:

```text
biz_content=GkTO5wRUFTij4GPniUQChP...&charset=UTF-8&format=JSON&language=pt&partner_id=200001835716&request_no=5298b3d713b6430c95729f9e54c967dc&service=instant_trade&timestamp=2022-01-20 19:44:51&version=1.0
```

#### 2.5.2 Calcular Assinatura a Partir do Texto Original

Os dados precisam ser assinados antes do envio. Coloque o resultado da assinatura no campo `sign`. Por favor, escolha **SHA1withRSA** como algoritmo de assinatura.

**Java**

```java
public String createSign(String text, String myPrivateKey) throws Exception {
    byte[] privateKeyBytes = Base64.getDecoder().decode(myPrivateKey);
    PKCS8EncodedKeySpec pkcs8KeySpec = new PKCS8EncodedKeySpec(privateKeyBytes);
    KeyFactory keyFactory = KeyFactory.getInstance("RSA");
    PrivateKey privateK = keyFactory.generatePrivate(pkcs8KeySpec);
    Signature signature = Signature.getInstance("SHA1withRSA");
    signature.initSign(privateK);
    signature.update(text.getBytes(StandardCharsets.UTF_8));
    byte[] result = signature.sign();
    return Base64.getEncoder().encodeToString(result);
}
```

**PHP**

```php
function createSign(string $text, string $myPrivateKey): string
{
    $pkcs8Bin = base64_decode($myPrivateKey);
    $pem = "-----BEGIN PRIVATE KEY-----\n" . chunk_split(base64_encode($pkcs8Bin), 64, "\n") . "-----END PRIVATE KEY-----\n";
    $privateKey = openssl_pkey_get_private($pem);
    if (!$privateKey) {
        throw new Exception('failed');
    }
    openssl_sign($text, $signBin, $privateKey, OPENSSL_ALGO_SHA1);
    return base64_encode($signBin);
}
```

**Node.js**

```js
const crypto = require('crypto');
function createSign(text, myPrivateKey) {
    const pkcs8Buf = Buffer.from(myPrivateKey, 'base64');
    const privateKey = crypto.createPrivateKey({
        key: pkcs8Buf,
        format: 'der',
        type: 'pkcs8'
    });
    const signer = crypto.createSign('sha1');
    signer.update(text, 'utf8');
    const signBuf = signer.sign(privateKey);
    return signBuf.toString('base64');
}
```

**Python**

```python
import base64
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.asymmetric import padding
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.backends import default_backend
from cryptography.hazmat.primitives import serialization

def createSign(text: str, myPrivateKey: str) -> str:
    pkcs8_der = base64.b64decode(myPrivateKey)
    private_key = serialization.load_der_private_key(
        pkcs8_der,
        password=None,
        backend=default_backend()
    )
    sign_bytes = private_key.sign(
        data=text.encode("utf-8"),
        padding=padding.PKCS1v15(),
        algorithm=hashes.SHA1()
    )
    return base64.b64encode(sign_bytes).decode("utf-8")
```

Após colocar o resultado calculado no parâmetro `sign`, você obterá o corpo completo da requisição.

### 2.6 Mais Uma Etapa Antes de Fazer a Requisição

Antes de enviar a requisição, exigimos que **todos os valores dos parâmetros sejam submetidos a urlencode uma vez**.

**Java**

```java
public String createRequestJsonWithUrlEncode() throws Exception {
    Map<String, String> map = new HashMap<>(this.params.size());
    for (Map.Entry<String, String> entry: this.params.entrySet()) {
        map.put(entry.getKey(), UrlEncoder.encode(entry.getValue()));
    }
    return JsonFormatter.toJSON(map);
}
```

**PHP**

```php
function createRequestJsonWithUrlEncode($params) {
    $result = [];
    foreach ($params as $key => $value) {
        $result[$key] = rawurlencode($value);
    }
    return json_encode($result, JSON_UNESCAPED_UNICODE);
}
```

**Node.js**

```js
function createRequestJsonWithUrlEncode(params) {
    const result = {};
    for (const key in params) {
        if (params.hasOwnProperty(key)) {
            const value = params[key];
            result[key] = encodeURIComponent(value);
        }
    }
    return JSON.stringify(result);
}
```

**Python**

```python
import json
from urllib.parse import quote

def create_request_json_with_url_encode(params: dict) -> str:
    result = {}
    for key, value in params.items():
        result[key] = quote(str(value))
    return json.dumps(result, ensure_ascii=False)
```

Usando o JSON acima como exemplo, o corpo final da requisição deve ser assim:

```json
{
    "charset":"UTF-8",
    "biz_content":"GkTO5wRUFTij4GPniUQChP...",
    "partner_id":"200001835716",
    "service":"instant_trade",
    "request_no":"5298b3d713b6430c95729f9e54c967dc",
    "format":"JSON",
    "sign":"N6EPWFhOOiqP7PfgEWgffszU6SCM...",
    "language":"pt",
    "sign_type":"RSA",
    "version":"1.0",
    "timestamp":"2022-01-20%2019%3A44%3A51"
}
```

### 2.7 Corpo da Resposta da API

Vamos primeiro ver um exemplo.

```json
{
    "code": "S0001",
    "sub_code": "S0001",
    "msg": "success",
    "sub_msg": "success",
    "sign": "g+DCRRN1s3sf+gD6k...",
    "charset": "UTF-8",
    "sign_type": "RSA",
    "biz_content": {
        "out_trade_no": "2022011812364864515635551",
        "trade_no": "101164267909194269883",
        "status": "P",
        "trade_token": "3b189ff399db4de4a24bae47bdadf4ef",
        "dynamic_link": "https://xxx.xxx/dynamic/link/3b189ff399db4de4a24bae47bdadf4ef"
    }
}
```

| Campo         | Descrição                                                                                                                                                                       |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `code`        | Código de erro. `S0001` indica sucesso, outros indicam erros (detalhados mais adiante)                                                                                          |
| `sub_code`    | Código de erro secundário (detalhado mais adiante)                                                                                                                              |
| `msg`         | Descrição do erro                                                                                                                                                               |
| `sub_msg`     | Descrição do erro secundário                                                                                                                                                    |
| `sign`        | Assinatura deste corpo de resposta. O algoritmo de assinatura é o mesmo descrito acima. Se precisar verificar a assinatura, use a chave pública do PayPay para verificação.     |
| `charset`     | Formato de codificação                                                                                                                                                          |
| `sign_type`   | Tipo de assinatura                                                                                                                                                              |
| `biz_content` | Informações de negócio. Se o processamento da interface falhar, geralmente este membro não existirá. As informações de negócio são diferentes para cada interface (ver abaixo). |

---

## 3. Descrição da API

### Visão Geral

| API                                   | Descrição                                                                                                                                                                                                                       |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 3.1 Pagamento Instantâneo             | Após a criação, pague através do aplicativo PayPay. O usuário pode usar vários métodos de pagamento no aplicativo PayPay, incluindo reference, express, saldo, etc. O comerciante não precisa se preocupar com o método de pagamento. |
| 3.2 Pagamento MULTICAIXA Express      | O comerciante especifica pagamento Express e pode interagir diretamente com o Express para concluir o pagamento. Por motivos de segurança, esta API atualmente requer qualificação para uso.                                     |
| 3.3 Pagamento MULTICAIXA Reference    | Da mesma forma que Express, o comerciante pode especificar o uso de Reference para pagamento. Atualmente, exigimos pedidos acima de 1000 Kz para usar esta API.                                                                  |
| 3.4 Reembolso                         | Comerciantes podem chamar esta API para concluir o reembolso, ajudando a resolver transações contestadas.                                                                                                                       |
| 3.5 Fechar Pedido                     | Se o pedido não for pago, o comerciante pode fechar ativamente o pedido. Após o fechamento, o usuário não pode pagar com o código QR ou link original.                                                                          |
| 3.6 Consultar Pedido                  | Permite consultar informações do pedido pelo número do pedido para entender o seu status.                                                                                                                                       |
| 3.7 Transferir para Conta Bancária    | Comerciantes podem iniciar pedidos para transferir dinheiro para a conta bancária especificada.                                                                                                                                 |
| 3.8 Transferir para Conta PayPay      | Comerciantes podem iniciar pedidos para transferir dinheiro para a conta PayPay especificada.                                                                                                                                   |
| 3.9 Pagamento Kwik QrCode             | O servidor Kwik fornece funcionalidade QrCode. O código QR gerado pode ser escaneado por vários aplicativos bancários ou outras carteiras para concluir o pagamento. O PayPay integrou esta funcionalidade e fornece uma API para comerciantes. |

---

### 3.1 Criar Pedido de Pagamento Instantâneo: `instant_trade`

A estrutura básica do corpo da requisição foi apresentada anteriormente e não será repetida aqui. Em caso de dúvidas, releia a introdução acima. Aqui o foco é o objeto de negócio. O mesmo se aplica às outras interfaces.

O valor do parâmetro `service` do corpo da requisição é: **`instant_trade`**.

#### 3.1.1 Objeto de Negócio

Exemplo:

```json
{
  "cashier_type": "SDK",
  "payer_ip": "123.25.68.9",
  "sale_product_code": "050200001",
  "timeout_express": "15m",
  "trade_info": {
    "currency": "AOA",
    "out_trade_no": "2022011812364864515635551",
    "payee_identity": "200001835716",
    "payee_identity_type": "1",
    "price": "10.00",
    "quantity": "1",
    "subject": "Catering expenses",
    "total_amount": "10.00"
  },
  "return_url": "https://www.google.com"
}
```

| Parâmetro                         | Obrigatório | Tipo         | Descrição                                                                                                                                                                                                                                                                                                                                                                                                                         |
| --------------------------------- | ----------- | ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `cashier_type`                    | Sim         | String(16)   | Valor fixo: `SDK`                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `payer_ip`                        | Sim         | String(6,32) | IP do usuário                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `sale_product_code`               | Sim         | String(9)    | Este valor é fornecido pelo PayPay. Após a assinatura do contrato, nossa equipe comercial o fornecerá                                                                                                                                                                                                                                                                                                                             |
| `timeout_express`                 | Não         | String(6)    | Quanto tempo o pedido espera pelo pagamento do usuário. Após ultrapassar o tempo, o pedido será fechado automaticamente e o usuário não poderá pagar através do código QR ou link original. `m`: minutos, `h`: horas, `d`: dias. Por exemplo, `40m` significa 40 minutos, `2h` significa 2 horas. Não são permitidos decimais (para 1,5 horas, passe `90m`, não `1.5h`). Faixa: `40m` a `7d`; padrão: `2h`                          |
| `trade_info.currency`             | Não         | String(3)    | Moeda, padrão AOA. Passe AOA; outras moedas não são suportadas                                                                                                                                                                                                                                                                                                                                                                    |
| `trade_info.out_trade_no`         | Sim         | String(6,32) | Seu número de pedido. Suporta números e letras. Certifique-se de que cada pedido tenha um número único                                                                                                                                                                                                                                                                                                                            |
| `trade_info.payee_identity`       | Sim         | String(32)   | Seu partner id. Forneça novamente aqui. Valor fixo                                                                                                                                                                                                                                                                                                                                                                                |
| `trade_info.payee_identity_type`  | Sim         | String(1)    | Valor fixo: `1`                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `trade_info.price`                | Sim         | String(15)   | Preço do produto. Mantenha no máximo duas casas decimais                                                                                                                                                                                                                                                                                                                                                                          |
| `trade_info.quantity`             | Sim         | String(5)    | Quantidade do produto                                                                                                                                                                                                                                                                                                                                                                                                             |
| `trade_info.subject`              | Sim         | String(256)  | Nome do produto                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `trade_info.total_amount`         | Sim         | String(15)   | Preço total: preço do produto × quantidade                                                                                                                                                                                                                                                                                                                                                                                        |
| `return_url`                      | Não         | String(500)  | Link ou urlschema para onde saltar após o usuário concluir o pagamento no aplicativo PayPay. Se não fornecido, não haverá salto. Se definir este valor, entre em contato com o suporte técnico do PayPay para configurar a lista branca; caso contrário, ocorrerão exceções                                                                                                                                                        |

#### 3.1.2 Objeto de Resposta da Interface

A estrutura de resposta da interface foi apresentada anteriormente. Aqui o foco é o objeto de negócio **biz_content**.

```json
{
    "code": "S0001",
    "sub_code": "S0001",
    "msg": "success",
    "sub_msg": "success",
    "sign": "g+DCRRN1s3sf+gD6k...",
    "charset": "UTF-8",
    "sign_type": "RSA",
    "biz_content": {
        "out_trade_no": "2022011812364864515635551",
        "trade_no": "101164267909194269883",
        "status": "P",
        "trade_token": "3b189ff399db4de4a24bae47bdadf4ef",
        "dynamic_link": "https://xxx.xxx/dynamic/link/3b189ff399db4de4a24bae47bdadf4ef"
    }
}
```

| Campo                       | Descrição                                                                                                                                                                                                                                              |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `biz_content.out_trade_no`  | Seu número de pedido, ou seja, o valor que você passou através do parâmetro `out_trade_no` acima                                                                                                                                                       |
| `biz_content.trade_no`      | Número do pedido PayPay. Recomenda-se salvar em seu banco de dados                                                                                                                                                                                     |
| `biz_content.status`        | Status. Não é o status final. O status final do pedido depende do processamento do sistema PayPay e será enviado ao seu sistema por meio de notificação assíncrona. Seu sistema também pode consultar ativamente o status do pedido                     |
| `biz_content.trade_token`   | Token de pagamento. Você pode usar o urlschema do nosso aplicativo `paypayao://trade/pay?action=pay&tradeToken=xxxxxxxxxxx` para ativar o PayPay e concluir o pagamento                                                                                 |
| `biz_content.dynamic_link`  | Pode ser usado para gerar um código QR e solicitar ao usuário que escaneie usando o aplicativo PayPay para concluir o pagamento. Se estiver no celular do usuário, usar o navegador do sistema para saltar também ativa com sucesso o aplicativo PayPay |

---

### 3.2 Criar Pedido MULTICAIXA Express: `instant_trade`

Como mencionado anteriormente, por motivos de segurança, o uso desta interface requer aprovação especial. Nem todos os comerciantes podem usá-la.

O valor do parâmetro `service` do corpo da requisição ainda é **`instant_trade`**, mas seus parâmetros de negócio são ligeiramente diferentes.

#### 3.2.1 Objeto de Negócio

```json
{
    "cashier_type": "SDK",
    "payer_ip": "123.25.68.9",
    "sale_product_code": "050200001",
    "timeout_express": "15m",
    "trade_info": {
        "currency": "AOA",
        "out_trade_no": "2022011812364864515635551",
        "payee_identity": "200001835716",
        "payee_identity_type": "1",
        "price": "10.00",
        "quantity": "1",
        "subject": "Catering expenses",
        "total_amount": "10.00"
    },
    "pay_method": {
        "pay_product_code": "31",
        "amount": "10.00",
        "bank_code": "MUL",
        "phone_num": "987654321"
    }
}
```

Os outros parâmetros são os mesmos da seção 3.1, mas há um parâmetro adicional: **`pay_method`**.

| Parâmetro                      | Obrigatório | Tipo       | Descrição                                                      |
| ------------------------------ | ----------- | ---------- | -------------------------------------------------------------- |
| `pay_method.pay_product_code`  | Sim         | String(2)  | Valor fixo: `31`                                               |
| `pay_method.amount`            | Sim         | String(15) | Valor do pagamento, igual ao valor do pedido                   |
| `pay_method.bank_code`         | Sim         | String(15) | Valor fixo: `MUL`                                              |
| `pay_method.phone_num`         | Sim         | String(9)  | Número de telefone do usuário cadastrado no MULTICAIXA Express |

#### 3.2.2 Objeto de Resposta da Interface

A estrutura de resposta da interface foi apresentada anteriormente. Aqui o foco é o objeto de negócio **biz_content**.

```json
{
    "code": "S0001",
    "sub_code": "S0001",
    "msg": "success",
    "sub_msg": "success",
    "sign": "g+DCRRN1s3sf+gD6k...",
    "charset": "UTF-8",
    "sign_type": "RSA",
    "biz_content": {
        "out_trade_no": "2022011812364864515635551",
        "trade_no": "101164267909194269883",
        "status": "P",
        "trade_token": "3b189ff399db4de4a24bae47bdadf4ef",
        "dynamic_link": "https://xxx.xxx/dynamic/link/3b189ff399db4de4a24bae47bdadf4ef"
    }
}
```

Como pode ver, é igual à interface 3.1. Quem está familiarizado com o pagamento MULTICAIXA Express sabe que o Express enviará notificações ao usuário para pagar. Durante este período, o aplicativo PayPay não precisa participar. Portanto, você pode ignorar os parâmetros **trade_token** e **dynamic_link**. O mesmo se aplica ao pagamento Reference abaixo.

---

### 3.3 Criar Pedido MULTICAIXA Reference: `instant_trade`

Atualmente, só permitimos pedidos **acima de 1000 Kz** neste canal. Caso contrário, será reportado um erro.

O valor do parâmetro `service` do corpo da requisição ainda é **`instant_trade`**, mas seus parâmetros de negócio são ligeiramente diferentes.

#### 3.3.1 Objeto de Negócio

```json
{
    "cashier_type": "SDK",
    "payer_ip": "123.25.68.9",
    "sale_product_code": "050200001",
    "timeout_express": "15m",
    "trade_info": {
        "currency": "AOA",
        "out_trade_no": "2022011812364864515635551",
        "payee_identity": "200001835716",
        "payee_identity_type": "1",
        "price": "10.00",
        "quantity": "1",
        "subject": "Catering expenses",
        "total_amount": "10.00"
    },
    "pay_method": {
        "pay_product_code": "31",
        "amount": "10.00",
        "bank_code": "REF"
    }
}
```

Os outros parâmetros são os mesmos da seção 3.1, mas há um parâmetro adicional: **`pay_method`**.

| Parâmetro                      | Obrigatório | Tipo       | Descrição                                    |
| ------------------------------ | ----------- | ---------- | -------------------------------------------- |
| `pay_method.pay_product_code`  | Sim         | String(2)  | Valor fixo: `31`                             |
| `pay_method.amount`            | Sim         | String(15) | Valor do pagamento, igual ao valor do pedido |
| `pay_method.bank_code`         | Sim         | String(15) | Valor fixo: `REF`                            |

#### 3.3.2 Objeto de Resposta da Interface

A estrutura de resposta da interface foi apresentada anteriormente. Aqui o foco é o objeto de negócio **biz_content**.

```json
{
    "code": "S0001",
    "sub_code": "S0001",
    "msg": "success",
    "sub_msg": "success",
    "sign": "g+DCRRN1s3s....",
    "charset": "UTF-8",
    "sign_type": "RSA",
    "biz_content": {
        "out_trade_no": "2022011812364864515635551",
        "trade_no": "101164267909194269883",
        "status": "P",
        "reference_id": "219485482",
        "entity_id": "01010",
        "trade_token": "3b189ff399db4de4a24bae47bdadf4ef",
        "dynamic_link": "https://xxx.xxx/dynamic/link/3b189ff399db4de4a24bae47bdadf4ef"
    }
}
```

Como pode ver, é semelhante à interface 3.1, mas há dois parâmetros adicionais: **reference_id** e **entity_id**. Mostre ao usuário estes dois códigos e solicite que use o MULTICAIXA Express para pagar. Durante este período, o aplicativo PayPay não precisa participar. Portanto, você pode ignorar os parâmetros **trade_token** e **dynamic_link**.

| Campo                      | Descrição                                                                                                        |
| -------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `biz_content.reference_id` | Número de Reference, 9 dígitos. Mostre este número ao usuário e também salve-o em seu sistema                    |
| `biz_content.entity_id`    | Entity ID. Mostre este número ao usuário e também salve-o em seu sistema                                         |

---

### 3.4 Iniciar Reembolso: `trade_refund`

O valor do parâmetro `service` do corpo da requisição é: **`trade_refund`**.

#### 3.4.1 Objeto de Negócio

Exemplo:

```json
{
    "orig_out_trade_no": "20220119125616493156531",
    "out_trade_no": "20220119125616493156531ref1",
    "refund_amount": "1.00"
}
```

| Parâmetro           | Obrigatório | Tipo         | Descrição                                                                                                                                 |
| ------------------- | ----------- | ------------ | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `orig_out_trade_no` | Sim         | String(6,32) | Número do pedido a ser reembolsado (o `out_trade_no` original)                                                                            |
| `out_trade_no`      | Sim         | String(6,32) | Número do pedido de reembolso, gerado por você. Mantenha a sua unicidade                                                                  |
| `refund_amount`     | Sim         | String(15)   | Valor do reembolso. Mantenha duas casas decimais. Não exceda o valor do pedido original                                                   |

#### 3.4.2 Objeto de Resposta da Interface

A estrutura de resposta da interface foi apresentada anteriormente. Aqui o foco é o objeto de negócio **biz_content**.

```json
{
    "code": "S0001",
    "sub_code": "S0001",
    "msg": "success",
    "sub_msg": "success",
    "sign": "g+DCRRN1s3s....",
    "charset": "UTF-8",
    "sign_type": "RSA",
    "biz_content": {
        "out_trade_no": "20220119125616493156531ref1",
        "trade_no": "103164267996507769884",
        "status": "P"
    }
}
```

| Campo                      | Descrição                                                                                                                                                                                                             |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `biz_content.out_trade_no` | Número do pedido de reembolso que você passou                                                                                                                                                                         |
| `biz_content.trade_no`     | Número do pedido PayPay. Salve em seu sistema                                                                                                                                                                         |
| `biz_content.status`       | Status. Não é o status final. O status final depende do processamento do sistema PayPay e será enviado ao seu sistema por notificação assíncrona. Seu sistema também pode consultar ativamente o status do pedido      |

---

### 3.5 Fechar Pedido: `trade_close`

O valor do parâmetro `service` do corpo da requisição é: **`trade_close`**.

#### 3.5.1 Objeto de Negócio

Exemplo:

```json
{
    "out_trade_no": "2022011812364864515635551"
}
```

| Parâmetro      | Obrigatório | Tipo         | Descrição                                               |
| -------------- | ----------- | ------------ | ------------------------------------------------------- |
| `out_trade_no` | Sim         | String(6,32) | Número do pedido que você deseja fechar antecipadamente |

#### 3.5.2 Objeto de Resposta da Interface

A estrutura de resposta da interface foi apresentada anteriormente. Aqui o foco é o objeto de negócio **biz_content**.

```json
{
    "code": "S0001",
    "sub_code": "S0001",
    "msg": "success",
    "sub_msg": "success",
    "sign": "g+DCRRN1s3s....",
    "charset": "UTF-8",
    "sign_type": "RSA",
    "biz_content": {
        "out_trade_no": "2022011812364864515635551",
        "trade_no": "101164267909194269883"
    }
}
```

| Campo                      | Descrição                                      |
| -------------------------- | ---------------------------------------------- |
| `biz_content.out_trade_no` | Número do pedido a ser fechado que você passou |
| `biz_content.trade_no`     | Número do pedido PayPay                        |

Esta interface entra em vigor imediatamente. Após o pedido ser fechado, o usuário não pode pagar usando o link ou código QR original. Ao mesmo tempo, você também receberá uma notificação assíncrona de fechamento.

---

### 3.6 Consultar Pedido: `trade_query`

O valor do parâmetro `service` do corpo da requisição é: **`trade_query`**.

#### 3.6.1 Objeto de Negócio

Exemplo:

```json
{
    "out_trade_no": "2022011812364864515635551"
}
```

| Parâmetro      | Obrigatório | Tipo         | Descrição            |
| -------------- | ----------- | ------------ | -------------------- |
| `out_trade_no` | Sim         | String(6,32) | Seu número de pedido |

#### 3.6.2 Objeto de Resposta da Interface

A estrutura de resposta da interface foi apresentada anteriormente. Aqui o foco é o objeto de negócio **biz_content**.

```json
{
    "....": "....",
    "biz_content": {
        "amount": "10.00",
        "royalty_list": [],
        "out_trade_no": "2022011812364864515635551",
        "partner_id": "200001835716",
        "subject": "Catering expenses",
        "modify_time": "2022-01-20 12:44:51",
        "payee_name": "00000000001234",
        "seller_actual_amount": "10.00",
        "trade_no": "101164267909194269883",
        "payer_id": "anonymous",
        "payee_id": "200001835716",
        "status": "WAIT_BUYER_PAY"
    }
}
```

| Campo                  | Descrição                                     |
| ---------------------- | --------------------------------------------- |
| `out_trade_no`         | Seu número de pedido                          |
| `trade_no`             | Número do pedido PayPay                       |
| `subject`              | Nome do produto                               |
| `partner_id`           | Partner ID do comerciante                     |
| `payee_id`             | ID do comerciante                             |
| `payee_name`           | Nome do comerciante                           |
| `amount`               | Valor do pedido                               |
| `seller_actual_amount` | Valor real pago                               |
| `status`               | Status do pedido (ver tabela abaixo)          |
| `modify_time`          | Hora de modificação                           |

#### 3.6.3 Status do Pedido

| Valor do Status         | Descrição                                                       |
| ----------------------- | --------------------------------------------------------------- |
| `WAIT_BUYER_PAY`        | Pedido criado com sucesso. Aguardando pagamento do comprador    |
| `TRADE_CLOSED`          | Transação fechada                                               |
| `TRADE_SUCCESS`         | Pagamento bem-sucedido                                          |
| `TRADE_FINISHED`        | Pagamento concluído. Fundos depositados na conta do comerciante |
| `REFUND_REQUEST_SUCCESS`| Solicitação de reembolso enviada com sucesso                    |
| `REFUND_SUCCESS`        | Reembolso concluído                                             |
| `REFUND_FAIL`           | Reembolso falhou                                                |

---

### 3.7 Transferir para Cartão Bancário: `transfer_to_card`

O valor do parâmetro `service` do corpo da requisição é: **`transfer_to_card`**.

#### 3.7.1 Objeto de Negócio

Exemplo:

```json
{
    "out_trade_no": "2022070607591644444",
    "payer_identity_type": "1",
    "payer_identity": "200002125756",
    "amount": "14.00",
    "currency": "AOA",
    "bank_card_no": "AO06005100000011111177881",
    "bank_account_name": "BIC Account Name Test",
    "bank_code": "BIC",
    "sale_product_code": "XXXXXXXXX",
    "pay_product_code": "11",
    "memo": "Test"
}
```

| Parâmetro             | Obrigatório | Tipo         | Descrição                                                                                                                                            |
| --------------------- | ----------- | ------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `out_trade_no`        | Sim         | String(6,32) | Seu número de pedido                                                                                                                                 |
| `payer_identity_type` | Sim         | String(1)    | Valor fixo: `1`                                                                                                                                      |
| `payer_identity`      | Sim         | String(32)   | Seu partner id                                                                                                                                       |
| `amount`              | Sim         | String(15)   | Valor da transferência. Mantenha no máximo duas casas decimais                                                                                       |
| `currency`            | Sim         | String(3)    | Moeda. Fixo `AOA`                                                                                                                                    |
| `bank_card_no`        | Sim         | String(32)   | IBAN do usuário. Forneça o IBAN completo começando com `AO06`. Remova todos os espaços, pontos e outros caracteres separadores                      |
| `bank_account_name`   | Sim         | String(256)  | Nome da conta bancária. Não inclua caracteres especiais ou números                                                                                   |
| `bank_code`           | Sim         | String(16)   | Código do banco. Ver lista abaixo                                                                                                                    |
| `sale_product_code`   | Sim         | String(16)   | O `sale_product_code` mencionado acima, fornecido pelo PayPay                                                                                        |
| `pay_product_code`    | Sim         | String(5)    | Valor fixo: `11`                                                                                                                                     |
| `memo`                | Não         | String(128)  | Informações da nota                                                                                                                                  |

#### 3.7.2 Lista de Bancos

Bancos suportados. Pode haver mudanças no futuro; consulte o PayPay para detalhes específicos.

O comerciante deve verificar se o formato do IBAN enviado pelo usuário está correto e se o IBAN corresponde ao banco correto. Caso contrário, o pedido não será aceito por nós.

Se o usuário adicionou informações de IBAN, você pode salvá-las para evitar que o usuário preencha essas informações novamente.

| Código   | Banco                                       | Prefixo IBAN |
| -------- | ------------------------------------------- | ------------ |
| `BAI`    | Banco Angolano de Investimentos             | AO060040     |
| `BFA`    | Banco de Fomento Angola                     | AO060006     |
| `YETU`   | Banco Yetu                                  | AO060066     |
| `BANC`   | Banco Angolano de Negócios e Comércio       | AO060053     |
| `BMF`    | Banco BAI Microfinanças                     | AO060048     |
| `BIC`    | Banco BIC                                   | AO060051     |
| `BCA`    | Banco Comercial Angolano                    | AO060043     |
| `BCH`    | Banco Comercial do Huambo                   | AO060059     |
| `BCI`    | Banco de Comércio e Indústria               | AO060005     |
| `BDA`    | Banco de Desenvolvimento Angola             | AO060054     |
| `BIR`    | Banco de Investimento Rural                 | AO060067     |
| `BNI`    | Banco de Negócios Internacional             | AO060052     |
| `BPC`    | Banco de Poupança e Crédito                 | AO060010     |
| `BE`     | Banco Económico                             | AO060045     |
| `KEVE`   | Banco Keve                                  | AO060047     |
| `BKI`    | Banco Kwanza Investimento                   | AO060057     |
| `BPG`    | Banco Prestígio                             | AO060064     |
| `BPA`    | Banco Millennium Atlântico                  | AO060055     |
| `BMAIS`  | Banco Mais                                  | AO060065     |
| `BSOL`   | Banco Sol                                   | AO060044     |
| `BVB`    | Banco Valor                                 | AO060062     |
| `FNB`    | Finibanco Angola                            | AO060058     |
| `SBA`    | Standard Bank de Angola                     | AO060060     |
| `VTB`    | Banco VTB África                            | AO060056     |
| `BCGA`   | Banco Caixa Geral de Angola                 | AO060004     |
| `SCBA`   | Standard Chartered Bank de Angola           | AO060063     |
| `CCP`    | CCP                                         | AO060000     |
| `USPM`   | USPM                                        | AO060417     |
| `BCS`    | BCS                                         | AO060070     |
| `PAYPAY` | PAYPAY                                      | AO060420     |

#### 3.7.3 Objeto de Resposta da Interface

A estrutura de resposta da interface foi apresentada anteriormente. Aqui o foco é o objeto de negócio **biz_content**.

```json
{
    "....": "....",
    "biz_content": {
        "out_trade_no": "20220705084341298111",
        "trade_no": "101165700776857695907",
        "status": "P"
    }
}
```

| Campo                      | Descrição                                                                                                                                                                                                             |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `biz_content.out_trade_no` | Número do pedido que você passou                                                                                                                                                                                      |
| `biz_content.trade_no`     | Número do pedido PayPay. Salve em seu sistema                                                                                                                                                                         |
| `biz_content.status`       | Status. Não é o status final. O status final depende do processamento do sistema PayPay e será enviado ao seu sistema por notificação assíncrona. Seu sistema também pode consultar ativamente o status do pedido      |

---

### 3.8 Transferir para Conta PayPay: `transfer_to_account`

O valor do parâmetro `service` do corpo da requisição é: **`transfer_to_account`**.

#### 3.8.1 Objeto de Negócio

Exemplo:

```json
{
    "out_trade_no": "2022070607591644444",
    "payer_identity_type": "1",
    "payer_identity": "200002125756",
    "payee_identity_type": "1",
    "payee_identity": "200002125777",
    "aging": "R",
    "transfer_amount": "14.00",
    "currency": "AOA",
    "sale_product_code": "XXXXXXXXX",
    "memo": "Test"
}
```

| Parâmetro             | Obrigatório | Tipo         | Descrição                                                                    |
| --------------------- | ----------- | ------------ | ---------------------------------------------------------------------------- |
| `out_trade_no`        | Sim         | String(6,32) | Seu número de pedido                                                         |
| `payer_identity_type` | Sim         | String(1)    | Valor fixo: `1`                                                              |
| `payer_identity`      | Sim         | String(32)   | Seu partner id                                                               |
| `payee_identity_type` | Sim         | String(1)    | Valor fixo: `2` *(o exemplo acima usa `1`; na dúvida, confirme com o PayPay)* |
| `payee_identity`      | Sim         | String(32)   | Nome de login do destinatário. Para usuários comuns, é o número de telefone  |
| `aging`               | Sim         | String(1)    | Valor fixo: `R`                                                              |
| `transfer_amount`     | Sim         | String(15)   | Valor da transferência. Mantenha no máximo duas casas decimais               |
| `currency`            | Sim         | String(3)    | Moeda. Fixo `AOA`                                                            |
| `sale_product_code`   | Sim         | String(16)   | O `sale_product_code` mencionado acima, fornecido pelo PayPay                |
| `memo`                | Não         | String(128)  | Informações da nota                                                          |

#### 3.8.2 Objeto de Resposta da Interface

A estrutura de resposta da interface foi apresentada anteriormente. Aqui o foco é o objeto de negócio **biz_content**.

```json
{
    "biz_content": {
        "out_trade_no": "20220705084341298111",
        "trade_no": "101165700776857695907",
        "order_time": "2022-07-05 08:49:08",
        "status": "P"
    }
}
```

| Campo                      | Descrição                                                                                                                                                                                                             |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `biz_content.out_trade_no` | Número do pedido que você passou                                                                                                                                                                                      |
| `biz_content.trade_no`     | Número do pedido PayPay. Salve em seu sistema                                                                                                                                                                         |
| `biz_content.order_time`   | Hora do pedido                                                                                                                                                                                                        |
| `biz_content.status`       | Status. Não é o status final. O status final depende do processamento do sistema PayPay e será enviado ao seu sistema por notificação assíncrona. Seu sistema também pode consultar ativamente o status do pedido      |

---

### 3.9 Criar Pedido de Código QR Kwik: `instant_trade`

O valor do parâmetro `service` do corpo da requisição é: **`instant_trade`**.

O Kwik fornece serviço de código QR. O código QR gerado a partir do seu link pode ser escaneado por qualquer outro aplicativo que suporte Kwik, incluindo o próprio aplicativo PayPay. Se tiver essa necessidade, pode consegui-lo através desta integração. Semelhante a 3.2 (MUL) e 3.3 (REF), adicionando alguns parâmetros à base de 3.1, você obterá um link Kwik. Então, pode usar este link para gerar o código QR.

Para sua conveniência, também fornecemos o link do código QR do PayPay através desta interface. A sua função é exatamente a mesma de 3.1.

#### 3.9.1 Objeto de Negócio

Exemplo:

```json
{
    "cashier_type": "SDK",
    "payer_ip": "123.25.68.9",
    "sale_product_code": "050200001",
    "timeout_express": "15m",
    "trade_info": {
        "currency": "AOA",
        "out_trade_no": "2022011812364864515635551",
        "payee_identity": "200001835716",
        "payee_identity_type": "1",
        "price": "10.00",
        "quantity": "1",
        "subject": "Catering expenses",
        "total_amount": "10.00"
    },
    "pay_method": {
        "pay_product_code": "31",
        "amount": "10.00",
        "bank_code": "QRCODE"
    }
}
```

Os outros parâmetros são os mesmos da seção 3.1, mas há um parâmetro adicional: **`pay_method`**.

| Parâmetro                      | Obrigatório | Tipo       | Descrição                                    |
| ------------------------------ | ----------- | ---------- | -------------------------------------------- |
| `pay_method.pay_product_code`  | Sim         | String(2)  | Valor fixo: `31`                             |
| `pay_method.amount`            | Sim         | String(15) | Valor do pagamento, igual ao valor do pedido |
| `pay_method.bank_code`         | Sim         | String(15) | Valor fixo: `QRCODE`                         |

#### 3.9.2 Objeto de Resposta da Interface

A estrutura de resposta da interface foi apresentada anteriormente. Aqui o foco é o objeto de negócio **biz_content**.

```json
{
    "biz_content": {
        "out_trade_no": "20260525140703058797",
        "trade_no": "101177971452843319031",
        "status": "P",
        "trade_token": "d7d08e79b7684f0383a98b43470e8959",
        "dynamic_link": "http://xxx.xxx/gateway/dynamic/link/d7d08e79b7684f0383a98b43470e8959",
        "paypay_qr_id": "___useless__",
        "paypay_link": "___useless__",
        "link": "https://qr.kwik.emis.ao/1/p03/KW1/iKnewVfg",
        "uniqueId": "iKnewVfg",
        "numUniqueId": "484510359",
        "iban": "AO06********1234",
        "name": "Your Name"
    }
}
```

| Campo                      | Descrição                                                                                                                                                                                                             |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `biz_content.out_trade_no` | Número do pedido que você passou                                                                                                                                                                                      |
| `biz_content.trade_no`     | Número do pedido PayPay. Salve em seu sistema                                                                                                                                                                         |
| `biz_content.status`       | Status. Não é o status final. O status final depende do processamento do sistema PayPay e será enviado ao seu sistema por notificação assíncrona. Seu sistema também pode consultar ativamente o status do pedido      |
| `biz_content.trade_token`  | Consulte a resposta de 3.1                                                                                                                                                                                            |
| `biz_content.dynamic_link` | Consulte a resposta de 3.1                                                                                                                                                                                            |
| `biz_content.link`         | Link do código QR KWiK. Pode ser usado para gerar o código QR                                                                                                                                                         |
| `biz_content.paypay_qr_id` | Inútil. Pode ignorar este campo                                                                                                                                                                                       |
| `biz_content.paypay_link`  | Inútil. Pode ignorar este campo                                                                                                                                                                                       |
| `biz_content.uniqueId`     | Inútil. Pode ignorar este campo                                                                                                                                                                                       |
| `biz_content.numUniqueId`  | Elemento de exibição do código QR. Sem outras utilidades                                                                                                                                                              |
| `biz_content.iban`         | Elemento de exibição do código QR. Sem outras utilidades                                                                                                                                                              |
| `biz_content.name`         | Elemento de exibição do código QR. Sem outras utilidades                                                                                                                                                              |

#### 3.9.3 Especificações do Código QR

O Kwik fornece uma especificação de referência do código QR, que pode ser usada como guia.

| Item                 | Conteúdo                                                                                                                                              |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| Imagem de Referência | [Ver imagem](https://portal.paypayafrica.com/dist/guide/img/kwikqr.png)                                                                               |
| Elemento 1           | [Ver imagem](https://portal.paypayafrica.com/dist/guide/img/bfd8f362343f41dfa430a193ec97810b.png)                                                     |
| Elemento 2           | [Ver imagem](https://portal.paypayafrica.com/dist/guide/img/05fb903132804361749f37cf57cf3682.png)                                                     |
| Elemento 3           | Texto fixo: `Scan KWiK`                                                                                                                               |
| Elemento 4           | [Ver imagem](https://portal.paypayafrica.com/dist/guide/img/fae6843597aed2ad76656f62378e0132.png)                                                     |
| Elemento 5           | Campo `name` da resposta da interface                                                                                                                 |
| Elemento 6           | Campo `iban` da resposta da interface                                                                                                                 |
| Elemento 7           | Campo `numUniqueId` da resposta da interface                                                                                                          |

![Imagem de referência do QR Kwik](https://portal.paypayafrica.com/dist/guide/img/kwikqr.png)

---

### 3.10 Criar Pedido de Agente PayPay: `instant_trade`

O valor do parâmetro `service` no corpo do pedido é: **`instant_trade`**.

O PayPay está a desenvolver o serviço de agentes. Os agentes PayPay ajudam pessoas que não têm a App PayPay ou nem mesmo um smartphone, realizando serviços como pagamentos, levantamentos e recargas através do PayPay.

Agora o PayPay disponibiliza aos comerciantes uma funcionalidade para concluir pedidos através dos agentes PayPay, permitindo aos comerciantes oferecer serviços a um público mais amplo.

#### 3.10.1 Lógica de Negócio

1. O utilizador final não utiliza a App PayPay, mas conhece um agente PayPay próximo que pode ajudá-lo
2. O utilizador final seleciona este método de pagamento na App do comerciante, correspondendo ao pagamento através desta API
3. Preencha o número de telefone e o nome completo do utilizador (estas informações podem ser pré-preenchidas caso já existam no vosso sistema)
4. A interface devolve um código de 9 dígitos. Apresente este código e peça ao utilizador para o guardar
5. O utilizador final entrega este código ao agente e efetua o pagamento em dinheiro ao agente
6. O agente obtém o pedido através deste código e conclui o pagamento com a sua conta PayPay; o pedido fica concluído

Os comerciantes não precisam conhecer os detalhes do fluxo de pagamento. Apenas aguarde a conclusão do pedido tal como na API 3.1.

#### 3.10.2 Objeto de Negócio

Exemplo de payload:

```json
{
    "cashier_type": "SDK",
    "payer_ip": "123.25.68.9",
    "sale_product_code": "050200001",
    "timeout_express": "15m",
    "trade_info": {
        "currency": "AOA",
        "out_trade_no": "2022011812364864515635551",
        "payee_identity": "200001835716",
        "payee_identity_type": "1",
        "price": "10.00",
        "quantity": "1",
        "subject": "Despesas de restauração",
        "total_amount": "10.00"
    },
    "pay_method": {
        "pay_product_code": "31",
        "amount": "10.00",
        "bank_code": "AGENT",
        "phone_num": "987654321",
        "memo": "Nome do User"
    }
}
```

Semelhante às APIs 3.2 e 3.3, apenas os campos de `pay_method` foram alterados.

| Parâmetro                      | Obrigatório | Tipo       | Descrição                                                   |
| ------------------------------ | ----------- | ---------- | ----------------------------------------------------------- |
| `pay_method.pay_product_code`  | Sim         | String(2)  | Valor fixo: `31`                                            |
| `pay_method.amount`            | Sim         | String(15) | Valor do pagamento, deve ser igual ao valor total do pedido |
| `pay_method.bank_code`         | Sim         | String(15) | Valor fixo: `AGENT`                                         |
| `pay_method.phone_num`         | Sim         | String(9)  | Número de telefone do utilizador                            |
| `pay_method.memo`              | Sim         | String(32) | Nome completo do utilizador                                 |

#### 3.10.3 Objeto de Resposta da API

A estrutura geral da resposta já foi explicada anteriormente. Abaixo foca-se o objeto de negócio **biz_content**.

```json
{
    "biz_content": {
        "out_trade_no": "20260525140703058797",
        "trade_no": "101177971452843319031",
        "status": "P",
        "agent_transaction_code": "364456322"
    }
}
```

| Campo                                 | Descrição                                                                                                                                                             |
| ------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `biz_content.out_trade_no`            | Número de pedido enviado pelo comerciante                                                                                                                             |
| `biz_content.trade_no`                | Número de pedido PayPay, guarde-o no seu sistema                                                                                                                      |
| `biz_content.status`                  | Estado do pedido. Não é um estado final. O estado final é enviado por notificação assíncrona pelo PayPay; também pode consultar o estado do pedido de forma ativa      |
| `biz_content.agent_transaction_code`  | Código de transação do agente, apresente este código ao utilizador final                                                                                              |

#### 3.10.4 Recomendações

Depois de submeter o pedido e obter o código de transação do agente, mostre as seguintes informações ao utilizador final:

- Número do pedido
- Valor do pedido
- Número de telefone do utilizador
- Nome completo do utilizador
- Código de transação do agente devolvido pela API

---

### 3.11 Levantamento por Agente

O valor do parâmetro `service` no corpo do pedido é: **`transfer_to_card`**.

O PayPay está a desenvolver o serviço de agentes. Os agentes PayPay ajudam pessoas que não têm a App PayPay ou nem mesmo um smartphone, realizando serviços como pagamentos, levantamentos e recargas através do PayPay.

Agora o PayPay disponibiliza aos comerciantes uma funcionalidade para concluir pedidos através dos agentes PayPay, permitindo aos comerciantes oferecer serviços a um público mais amplo.

#### 3.11.1 Lógica de Negócio

1. O utilizador final não utiliza a App PayPay, mas conhece um agente PayPay próximo que pode ajudá-lo
2. O utilizador final seleciona este método de pagamento na App do comerciante, correspondendo ao pagamento através desta API
3. Preencha o número de telefone e o nome completo do utilizador (estas informações podem ser pré-preenchidas caso já existam no vosso sistema)
4. A interface devolve um código de 9 dígitos. Apresente este código e peça ao utilizador para o guardar
5. O utilizador final entrega este código ao agente
6. O agente obtém o pedido através deste código, recebe o valor do pedido e entrega o dinheiro em numerário ao utilizador final

Os comerciantes não precisam conhecer os detalhes do fluxo de pagamento. Apenas aguarde a conclusão do pedido tal como na API 3.7.

#### 3.11.2 Objeto de Negócio

Exemplo de payload:

```json
{
    "out_trade_no": "2022070607591644444",
    "payer_identity_type": "1",
    "payer_identity": "200002125756",
    "amount": "14.00",
    "currency": "AOA",
    "bank_card_no": "987654321",
    "bank_account_name": "Nome do User",
    "bank_code": "AGENT",
    "sale_product_code": "XXXXXXXXX",
    "pay_product_code": "11",
    "memo": "Teste"
}
```

Semelhante à API 3.7 (Levantamento para Cartão), alguns campos usam valores fixos especiais.

| Parâmetro             | Obrigatório | Tipo         | Descrição                                      |
| --------------------- | ----------- | ------------ | ---------------------------------------------- |
| `out_trade_no`        | Sim         | String(6,32) | Número de pedido do comerciante                |
| `payer_identity_type` | Sim         | String(1)    | Valor fixo: `1`                                |
| `payer_identity`      | Sim         | String(32)   | Seu partner id                                 |
| `amount`              | Sim         | String(15)   | Valor do pagamento, máximo duas casas decimais |
| `currency`            | Sim         | String(3)    | Moeda, valor fixo `AOA`                        |
| `bank_card_no`        | Sim         | String(32)   | Número de telefone do utilizador               |
| `bank_account_name`   | Sim         | String(256)  | Nome completo do utilizador                    |
| `bank_code`           | Sim         | String(16)   | Valor fixo: `AGENT`                            |
| `sale_product_code`   | Sim         | String(16)   | `sale_product_code` fornecido pelo PayPay      |
| `pay_product_code`    | Sim         | String(5)    | Valor fixo: `11`                               |
| `memo`                | Não         | String(128)  | Observação                                     |

#### 3.11.3 Objeto de Resposta da API

A estrutura geral da resposta já foi explicada anteriormente. Abaixo foca-se o objeto de negócio **biz_content**.

```json
{
    "biz_content": {
        "out_trade_no": "20260525140703058797",
        "trade_no": "101177971452843319031",
        "status": "P",
        "agent_transaction_code": "495296958"
    }
}
```

| Campo                                 | Descrição                                                                                                                                                             |
| ------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `biz_content.out_trade_no`            | Número de pedido enviado pelo comerciante                                                                                                                             |
| `biz_content.trade_no`                | Número de pedido PayPay, guarde-o no seu sistema                                                                                                                      |
| `biz_content.status`                  | Estado do pedido. Não é um estado final. O estado final é enviado por notificação assíncrona pelo PayPay; também pode consultar o estado do pedido de forma ativa      |
| `biz_content.agent_transaction_code`  | Código de transação do agente, apresente este código ao utilizador final                                                                                              |

#### 3.11.4 Recomendações

Depois de submeter o pedido e obter o código de transação do agente, mostre as seguintes informações ao utilizador final:

- Número do pedido
- Valor do levantamento
- Número de telefone do utilizador
- Nome completo do utilizador
- Código de transação do agente devolvido pela API

---

## 4. Notificações Assíncronas

Como mencionado acima, o status final do pedido é baseado em notificações assíncronas ou na API de consulta de pedidos (3.6).

Comparadas com a API de consulta, as notificações assíncronas ajudam-no a saber quando o pedido mudou. Por exemplo, quando o usuário conclui o pagamento, a notificação assíncrona notificará primeiro o seu servidor.

Para implementar a função de notificação assíncrona, precisamos que você forneça a URL de notificação assíncrona na interface **API Setting** em nosso portal. Enviaremos as informações de status do pedido e outras informações para este endereço via POST.

Quando você concluir o processamento, precisa responder: `success` (sem nenhum outro espaço ou tag HTML).

Se não recebermos a resposta `success`, faremos **7 tentativas**, respectivamente em **2, 10, 10, 60, 120, 360 e 900 minutos**.

Também exigimos que seu servidor processe rapidamente nossas requisições de notificação. Uma resposta muito lenta pode causar timeout de rede. Em caso de timeout, tentaremos apenas mais uma vez.

Por favor, defina a URL de notificação:

![Captura de Tela](https://portal.paypayafrica.com/dist/guide/img/image-20220126075426593.png)

### 4.1 Objeto de Negócio

A seguir estão os dados que enviamos para a sua URL via POST. Note que enviamos no formato `application/x-www-form-urlencoded`, **não em JSON**.

#### 4.1.1 Exemplo

| Campo            | Valor                               |
| ---------------- | ----------------------------------- |
| `gmt_create`     | `1641546460908`                     |
| `amount`         | `100.37`                            |
| `gmt_payment`    | `1641546465878`                     |
| `notify_time`    | `20220120111833`                    |
| `payerIdentity`  | `924***323`                         |
| `role`           | `partner`                           |
| `input_charset`  | `UTF-8`                             |
| `sign`           | `jlDVUZsllFqa5LxXwQncGy/YkpulP....` |
| `notify_create`  | `1642673913264`                     |
| `notify_id`      | `a146a25b97b14399a20acfe24bfccaaa`  |
| `notify_type`    | `trade_status_sync`                 |
| `payeeIdentity`  | `l******n@zsaipay.com`              |
| `out_trade_no`   | `2475996115`                        |
| `inner_trade_no` | `101164154646089866469`             |
| `gmt_close`      | `1642673910000`                     |
| `sign_type`      | `RSA`                               |
| `status`         | `TRADE_SUCCESS`                     |

#### 4.1.2 Descrição dos Campos

| Parâmetro           | Obrigatório | Tipo        | Descrição                                                                                       |
| ------------------- | ----------- | ----------- | ----------------------------------------------------------------------------------------------- |
| `notify_id`         | Sim         | String(32)  | Número da notificação assíncrona                                                                |
| `notify_type`       | Sim         | String(32)  | Tipo de notificação. Valor fixo: `trade_status_sync`                                            |
| `notify_create`     | Sim         | String(14)  | Hora da notificação (timestamp)                                                                 |
| `input_charset`     | Sim         | String(10)  | Codificação. Valor fixo: `UTF-8`                                                                |
| `sign`              | Sim         | String(256) | Assinatura                                                                                      |
| `sign_type`         | Sim         | String(10)  | Valor fixo: `RSA`                                                                               |
| `version`           | Não         | String(10)  | Valor fixo: `1.0`                                                                               |
| `out_trade_no`      | Sim         | String(32)  | Número do pedido do comerciante                                                                 |
| `inner_trade_no`    | Sim         | String(32)  | Número do pedido PayPay                                                                         |
| `orig_out_trade_no` | Não         | String(32)  | Presente em notificações de reembolso. O seu valor é o número do pedido original                |
| `status`            | Sim         | String(32)  | Status do pedido (ver tabela abaixo)                                                            |
| `amount`            | Sim         | String(32)  | Valor do pedido                                                                                 |
| `role`              | Não         | String(32)  | Papel da notificação                                                                            |
| `payerIdentity`     | Não         | String(32)  | Conta do pagador                                                                                |
| `payeeIdentity`     | Não         | String(32)  | Conta do beneficiário                                                                           |
| `gmt_create`        | Sim         | String(14)  | Hora de criação do pedido                                                                       |
| `gmt_payment`       | Não         | String(14)  | Hora de pagamento do pedido                                                                     |
| `gmt_close`         | Não         | String(14)  | Hora de fechamento do pedido                                                                    |
| `failReason`        | Não         | String(100) | Motivo da falha                                                                                 |
| `failCode`          | Não         | String(16)  | Código de erro da falha                                                                         |

#### 4.1.3 Sobre o Campo `sign`

O algoritmo de assinatura é o mesmo mencionado acima, mas note que agora **você é a parte que verifica a assinatura**. Você precisa usar a **chave pública do PayPay** para verificar a assinatura, não a sua própria chave pública. A chave pública do PayPay pode ser baixada nas Configurações de API do portal.

**Java**

```java
public static boolean verifySign(String orgText, String signText, String paypayPublicKey) throws Exception {
    byte[] keyBytes = Base64.getDecoder().decode(paypayPublicKey);
    X509EncodedKeySpec keySpec = new X509EncodedKeySpec(keyBytes);
    KeyFactory keyFactory = KeyFactory.getInstance("RSA");
    PublicKey publicK = keyFactory.generatePublic(keySpec);
    Signature signature = Signature.getInstance("SHA1withRSA");
    signature.initVerify(publicK);
    signature.update(orgText.getBytes(StandardCharsets.UTF_8));
    return signature.verify(Base64.getDecoder().decode(signText));
}
```

**PHP**

```php
function verifySign($orgText, $signText, $paypayPublicKey) {
    if (strpos($paypayPublicKey, '-----BEGIN PUBLIC KEY-----') === false) {
        $paypayPublicKey = "-----BEGIN PUBLIC KEY-----\n" .
                           wordwrap($paypayPublicKey, 64, "\n", true) .
                           "\n-----END PUBLIC KEY-----";
    }
    $pubKeyResource = openssl_pkey_get_public($paypayPublicKey);
    if (!$pubKeyResource) {
        throw new Exception("Invalid public key");
    }
    $signatureBytes = base64_decode($signText);
    $result = openssl_verify($orgText, $signatureBytes, $pubKeyResource, OPENSSL_ALGO_SHA1);
    return $result === 1;
}
```

**Node.js**

```js
const crypto = require('crypto');
function verifySign(orgText, signText, paypayPublicKey) {
    try {
        let pemKey = paypayPublicKey;
        if (!paypayPublicKey.includes('-----BEGIN PUBLIC KEY-----')) {
            pemKey = `-----BEGIN PUBLIC KEY-----\n${paypayPublicKey.match(/.{1,64}/g).join('\n')}\n-----END PUBLIC KEY-----`;
        }
        const verify = crypto.createVerify('RSA-SHA1');
        verify.update(Buffer.from(orgText, 'utf8'));
        return verify.verify(pemKey, signText, 'base64');
    } catch (error) {
        console.error("Verification error:", error);
        return false;
    }
}
```

**Python**

```python
import base64
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.asymmetric import padding
from cryptography.hazmat.primitives.serialization import load_der_public_key, load_pem_public_key

def verify_sign(org_text, sign_text, paypay_public_key):
    try:
        key_bytes = base64.b64decode(paypay_public_key)
        public_key = load_der_public_key(key_bytes)
        # public_key = load_pem_public_key(paypay_public_key.encode('utf-8'))
        org_bytes = org_text.encode('utf-8')
        sign_bytes = base64.b64decode(sign_text)
        public_key.verify(
            sign_bytes,
            org_bytes,
            padding.PKCS1v15(),
            hashes.SHA1()
        )
        return True
    except Exception as e:
        print(f"Verify failed: {e}")
        return False
```

#### 4.1.4 Status de Trigger

**Notificações de 3.1, 3.2, 3.3, 3.9 (Criar Pedido) e 3.8 (Transferir para Conta PayPay)**

| Status           | Tipo de Notificação                           | Descrição                                                                                                                                                              |
| ---------------- | --------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TRADE_SUCCESS`  | Notificação assíncrona de status de pagamento | Pagamento bem-sucedido. **Recomenda-se ignorar esta notificação**, ou seja, ao recebê-la, responda `success` diretamente, sem processamento de negócio                 |
| `TRADE_FINISHED` | Notificação assíncrona de status de pagamento | Pagamento concluído e liquidado na conta empresarial. **Recomenda-se usar esta notificação como base para determinar se o pedido foi bem-sucedido**                    |
| `TRADE_CLOSED`   | Notificação assíncrona de status de pagamento | Pedido não pago por um longo tempo será fechado. Também representa falha do pedido                                                                                     |

**Notificações de 3.4 (Reembolso)**

| Status           | Tipo de Notificação                            | Descrição              |
| ---------------- | ---------------------------------------------- | ---------------------- |
| `REFUND_SUCCESS` | Notificação assíncrona de status de reembolso  | Reembolso bem-sucedido |
| `REFUND_FAIL`    | Notificação assíncrona de status de reembolso  | Reembolso falhou       |

**Notificações de 3.7 (Transferir para Cartão Bancário)**

| Status             | Tipo de Notificação                                                    | Descrição                                                                                                                                         |
| ------------------ | ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TRANSFER_SUCCESS` | Notificação assíncrona de status de transferência para cartão bancário | Transferência para cartão bancário bem-sucedida                                                                                                   |
| `TRANSFER_FAIL`    | Notificação assíncrona de status de transferência para cartão bancário | Transferência para cartão bancário falhou                                                                                                         |
| `RETURN_TICKET`    | Notificação assíncrona de status de transferência para cartão bancário | A transferência para o banco teve devolução. Atualmente não acontece. Ou seja, após a transferência ser bem-sucedida, o banco recusou o pedido    |
