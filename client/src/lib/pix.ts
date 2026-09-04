// Gera o payload "Pix Copia e Cola" (BR Code / EMV QR Code) a partir de uma
// chave Pix estática, seguindo o Manual de Padrões para Iniciação do Pix
// (Bacen). Sem valor fixo: quem paga digita o valor no app do banco.

function tlv(id: string, value: string): string {
  const len = value.length.toString().padStart(2, "0");
  return `${id}${len}${value}`;
}

// CRC16/CCITT-FALSE (poly 0x1021, init 0xFFFF), exigido no campo final (63).
function crc16(payload: string): string {
  let crc = 0xffff;
  for (let i = 0; i < payload.length; i++) {
    crc ^= payload.charCodeAt(i) << 8;
    for (let j = 0; j < 8; j++) {
      crc = (crc & 0x8000) !== 0 ? ((crc << 1) ^ 0x1021) & 0xffff : (crc << 1) & 0xffff;
    }
  }
  return crc.toString(16).toUpperCase().padStart(4, "0");
}

// Remove acentos e caracteres fora do padrão aceito pelos campos 59/60.
function normalize(value: string, maxLen: number): string {
  const stripped = value
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "") // remove diacríticos (combining marks) após NFD
    .replace(/[^A-Za-z0-9 ]/g, "")
    .toUpperCase()
    .trim();
  return stripped.slice(0, maxLen);
}

export function buildPixPayload(opts: { key: string; merchantName: string; merchantCity: string; description?: string }): string {
  const keyDigits = opts.key.replace(/\D/g, "") || opts.key;
  const merchantAccount =
    tlv("00", "br.gov.bcb.pix") +
    tlv("01", keyDigits) +
    (opts.description ? tlv("02", normalize(opts.description, 40)) : "");

  const payloadWithoutCrc =
    tlv("00", "01") + // Payload Format Indicator
    tlv("01", "11") + // Point of Initiation Method: estático/reutilizável
    tlv("26", merchantAccount) + // Merchant Account Information — Pix
    tlv("52", "0000") + // Merchant Category Code
    tlv("53", "986") + // Moeda: Real (BRL)
    tlv("58", "BR") + // País
    tlv("59", normalize(opts.merchantName, 25)) + // Nome do recebedor
    tlv("60", normalize(opts.merchantCity, 15)) + // Cidade do recebedor
    tlv("62", tlv("05", "***")) + // Additional Data Field — txid genérico
    "6304"; // Início do campo CRC (id 63, tamanho 04)

  return payloadWithoutCrc + crc16(payloadWithoutCrc);
}
