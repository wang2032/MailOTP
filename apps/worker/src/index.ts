export interface Env {
  API_URL: string;
  WEBHOOK_SECRET: string;
  MAIL_DOMAIN?: string;
}

type EmailMessage = {
  from: string;
  to: string;
  raw: ReadableStream<Uint8Array>;
  headers: Headers;
};

const OTP_PATTERNS = [
  /\b\d{6}\b/,
  /\b\d{4,8}\b/,
  /\b[A-Z0-9]{5}\b/,
];

export default {
  async email(message: EmailMessage, env: Env): Promise<void> {
    const raw = await streamToText(message.raw);
    const subject = decodeMimeWords(getHeader(message.headers, "subject") || parseHeader(raw, "subject"));
    const alias = aliasFromAddress(message.to, env.MAIL_DOMAIN);
    const content = extractContent(raw).slice(0, 50_000);
    const code = extractCode(subject, content);
    const providerMessageId = getHeader(message.headers, "message-id") || parseHeader(raw, "message-id");

    await fetch(`${env.API_URL.replace(/\/+$/, "")}/mail`, {
      method: "POST",
      headers: {
        "Authorization": `Bearer ${env.WEBHOOK_SECRET}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        alias,
        recipient: message.to,
        sender: message.from,
        subject,
        code,
        content,
        provider_message_id: providerMessageId,
      }),
    }).then(async (response) => {
      if (!response.ok) {
        throw new Error(`API rejected mail: ${response.status} ${await response.text()}`);
      }
    });
  },
};

async function streamToText(stream: ReadableStream<Uint8Array>): Promise<string> {
  return new Response(stream).text();
}

function getHeader(headers: Headers, name: string): string {
  return headers.get(name)?.trim() || "";
}

function parseHeader(raw: string, name: string): string {
  const pattern = new RegExp(`^${escapeRegExp(name)}:\\s*(.+)$`, "im");
  return raw.match(pattern)?.[1]?.trim() || "";
}

function messageBody(raw: string): string {
  const normalized = raw.replace(/\r\n/g, "\n");
  const splitAt = normalized.indexOf("\n\n");
  return splitAt >= 0 ? normalized.slice(splitAt + 2) : normalized;
}

function extractContent(raw: string): string {
  const parts = extractMimeParts(raw);
  const candidates = parts.length > 0 ? parts : [raw];
  return candidates
    .map((part) => decodeTransferBody(part))
    .map(stripHTML)
    .map(decodeMimeWords)
    .join("\n\n")
    .trim();
}

function extractMimeParts(raw: string): string[] {
  const boundary = parseBoundary(raw);
  if (!boundary) {
    return [];
  }

  const normalized = raw.replace(/\r\n/g, "\n");
  const delimiter = `--${boundary}`;
  return normalized
    .split(delimiter)
    .filter((part) => part.trim() && !part.startsWith("--"))
    .map((part) => part.trim());
}

function parseBoundary(raw: string): string {
  const contentType = parseHeader(raw, "content-type");
  const match = contentType.match(/boundary=(?:"([^"]+)"|([^;\s]+))/i);
  return (match?.[1] || match?.[2] || "").trim();
}

function decodeTransferBody(value: string): string {
  const normalized = value.replace(/\r\n/g, "\n");
  const headers = normalized.slice(0, Math.max(0, normalized.indexOf("\n\n")));
  const encoding = parseHeader(headers, "content-transfer-encoding").toLowerCase();
  const body = normalized.includes("\n\n") ? messageBody(normalized) : normalized;

  if (encoding === "base64" || looksLikeBase64(body)) {
    return decodeBase64(body.replace(/\s+/g, "")) || body;
  }
  if (encoding === "quoted-printable" || body.includes("=3D") || body.includes("=\n")) {
    return decodeQuotedPrintable(body);
  }
  return body;
}

function looksLikeBase64(value: string): boolean {
  const compact = value.replace(/\s+/g, "");
  return compact.length >= 24 && compact.length % 4 === 0 && /^[A-Za-z0-9+/]+={0,2}$/.test(compact);
}

function decodeBase64(value: string): string {
  try {
    const binary = atob(value);
    const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
    return new TextDecoder().decode(bytes);
  } catch {
    return "";
  }
}

function decodeQuotedPrintable(value: string): string {
  const unfolded = value.replace(/=\n/g, "");
  const bytes: number[] = [];
  for (let index = 0; index < unfolded.length; index += 1) {
    if (unfolded[index] === "=" && /^[0-9A-F]{2}$/i.test(unfolded.slice(index + 1, index + 3))) {
      bytes.push(parseInt(unfolded.slice(index + 1, index + 3), 16));
      index += 2;
    } else {
      bytes.push(unfolded.charCodeAt(index));
    }
  }
  return new TextDecoder().decode(Uint8Array.from(bytes));
}

function stripHTML(value: string): string {
  return value
    .replace(/<style[\s\S]*?<\/style>/gi, " ")
    .replace(/<script[\s\S]*?<\/script>/gi, " ")
    .replace(/<[^>]+>/g, " ")
    .replace(/&nbsp;/gi, " ")
    .replace(/&amp;/gi, "&")
    .replace(/&lt;/gi, "<")
    .replace(/&gt;/gi, ">")
    .replace(/\s+/g, " ");
}

function decodeMimeWords(value: string): string {
  return value.replace(/=\?([^?]+)\?([BQ])\?([^?]+)\?=/gi, (_, charset: string, encoding: string, text: string) => {
    const bytes = encoding.toUpperCase() === "B"
      ? base64Bytes(text)
      : quotedPrintableBytes(text.replace(/_/g, " "));
    if (!bytes.length) {
      return text;
    }
    try {
      return new TextDecoder(charset.toLowerCase()).decode(Uint8Array.from(bytes));
    } catch {
      return new TextDecoder().decode(Uint8Array.from(bytes));
    }
  });
}

function base64Bytes(value: string): number[] {
  try {
    return Array.from(atob(value), (char) => char.charCodeAt(0));
  } catch {
    return [];
  }
}

function quotedPrintableBytes(value: string): number[] {
  const bytes: number[] = [];
  for (let index = 0; index < value.length; index += 1) {
    if (value[index] === "=" && /^[0-9A-F]{2}$/i.test(value.slice(index + 1, index + 3))) {
      bytes.push(parseInt(value.slice(index + 1, index + 3), 16));
      index += 2;
    } else {
      bytes.push(value.charCodeAt(index));
    }
  }
  return bytes;
}

function aliasFromAddress(address: string, expectedDomain?: string): string {
  const email = extractEmail(address).toLowerCase();
  const [local, domain] = email.split("@");
  if (!local || !domain) {
    throw new Error(`Invalid recipient address: ${address}`);
  }
  if (expectedDomain && domain !== expectedDomain.toLowerCase()) {
    throw new Error(`Unexpected recipient domain: ${domain}`);
  }
  return local;
}

function extractEmail(value: string): string {
  const bracketed = value.match(/<([^<>@\s]+@[^<>\s]+)>/);
  if (bracketed) {
    return bracketed[1];
  }
  const bare = value.match(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/i);
  if (bare) {
    return bare[0];
  }
  return value.trim();
}

function extractCode(...parts: string[]): string {
  const text = parts.filter(Boolean).join("\n");
  for (const pattern of OTP_PATTERNS) {
    const match = text.match(pattern);
    if (match) {
      return match[0];
    }
  }
  return "";
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
