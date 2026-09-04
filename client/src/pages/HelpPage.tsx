import { useState } from "react";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { buildPixPayload } from "@/lib/pix";
import { Copy, HeartHandshake, Instagram, Users, Youtube } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

// Chave PIX (CNPJ) usada para apoiar o projeto.
const PIX_KEY = "52.262.410/0001-04";
const PIX_MERCHANT_NAME = "RAPHAEL BATISTA SILVA";
// Cidade do recebedor não informada — usada como valor padrão do BR Code.
// Ajuste em @/lib/pix caso precise refletir a cidade real cadastrada na chave.
const PIX_MERCHANT_CITY = "BRASIL";
const INSTAGRAM_HANDLE = "orapha.batista";
const INSTAGRAM_URL = `https://www.instagram.com/${INSTAGRAM_HANDLE}/`;
const YOUTUBE_HANDLE = "vemfazer";
const YOUTUBE_URL = `https://www.youtube.com/@${YOUTUBE_HANDLE}`;
const SUPPORT_GROUP_URL = "https://chat.whatsapp.com/CFjwESjKfPhBpKG2ai6YmU";

// Busca a foto de perfil/capa atual direto da plataforma via unavatar.io
// (serviço público, sem chave de API) — assim a foto do canal/Instagram fica
// sempre atualizada automaticamente, sem precisar subir/trocar imagem aqui.
// `fallback=false` faz retornar 404 em vez de um avatar genérico quando não
// encontra, para o <img onError> assumir e mostrar o ícone da plataforma.
const YOUTUBE_AVATAR_URL = `https://unavatar.io/youtube/${YOUTUBE_HANDLE}?fallback=false`;
const INSTAGRAM_AVATAR_URL = `https://unavatar.io/instagram/${INSTAGRAM_HANDLE}?fallback=false`;

const PIX_PAYLOAD = buildPixPayload({
  key: PIX_KEY,
  merchantName: PIX_MERCHANT_NAME,
  merchantCity: PIX_MERCHANT_CITY,
});

const RESPONSIBLE_NAME = "Raphael Batista da Silva";

/** Ícone/avatar centralizado no topo do card: mostra a foto real da conta
 * quando o unavatar.io consegue resolvê-la, e cai para o ícone da
 * plataforma (Icon) caso contrário — sem quebrar o layout. */
const SocialAvatar = ({ src, alt, Icon }: { src: string; alt: string; Icon: typeof Youtube }) => {
  const [failed, setFailed] = useState(false);
  return (
    <div className="grid h-16 w-16 place-items-center overflow-hidden rounded-full bg-nav-icon/10 text-nav-icon">
      {!failed ? (
        <img src={src} alt={alt} className="h-full w-full object-cover" onError={() => setFailed(true)} />
      ) : (
        <Icon className="h-7 w-7" />
      )}
    </div>
  );
};

export default function HelpPage() {
  const { t } = useTranslation();

  const copyPix = () => {
    void navigator.clipboard.writeText(PIX_KEY.replace(/\D/g, ""));
    toast.success(t("pages.help.pixCopied", { defaultValue: "Chave PIX copiada" }));
  };

  return (
    <AppShell>
      <div className="mx-auto max-w-3xl space-y-8 p-2 text-center">
        <div className="space-y-2">
          <h2 className="text-3xl font-bold tracking-tight">
            {t("pages.help.title", { defaultValue: "Ajuda & Apoie o Projeto" })}
          </h2>
          <p className="text-muted-foreground">
            {t("pages.help.subtitle", {
              defaultValue: `Este sistema é mantido e desenvolvido por ${RESPONSIBLE_NAME}. Se ele te ajudou, considere apoiar o projeto abaixo.`,
              name: RESPONSIBLE_NAME,
            })}
          </p>
        </div>

        <Card>
          <CardHeader className="flex flex-col items-center gap-2 space-y-0 text-center">
            <SocialAvatar src={YOUTUBE_AVATAR_URL} alt={t("pages.help.youtubeTitle", { defaultValue: "Canal Vem Fazer" })} Icon={Youtube} />
            <div>
              <CardTitle className="text-lg">
                {t("pages.help.youtubeTitle", { defaultValue: "Canal Vem Fazer" })}
              </CardTitle>
              <CardDescription>
                {t("pages.help.youtubeDesc", { defaultValue: "Inscreva-se no canal para acompanhar novidades e tutoriais." })}
              </CardDescription>
            </div>
          </CardHeader>
          <CardContent className="flex flex-col items-center gap-2">
            <Button variant="outline" asChild>
              <a href={YOUTUBE_URL} target="_blank" rel="noopener noreferrer">
                {t("pages.help.youtubeButton", { defaultValue: "Inscrever-se no canal" })}
              </a>
            </Button>
            <Button variant="outline" asChild>
              <a href={SUPPORT_GROUP_URL} target="_blank" rel="noopener noreferrer">
                <Users className="mr-2 h-4 w-4" />
                {t("pages.help.groupButton", { defaultValue: "Entrar no grupo de apoio" })}
              </a>
            </Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-col items-center gap-2 space-y-0 text-center">
            <SocialAvatar src={INSTAGRAM_AVATAR_URL} alt={t("pages.help.instagramTitle", { defaultValue: "Siga o criador do projeto" })} Icon={Instagram} />
            <div>
              <CardTitle className="text-lg">
                {t("pages.help.instagramTitle", { defaultValue: "Siga o criador do projeto" })}
              </CardTitle>
              <CardDescription>
                {t("pages.help.instagramDesc", { defaultValue: "Acompanhe @orapha.batista no Instagram." })}
              </CardDescription>
            </div>
          </CardHeader>
          <CardContent className="flex justify-center">
            <Button variant="outline" asChild>
              <a href={INSTAGRAM_URL} target="_blank" rel="noopener noreferrer">
                {t("pages.help.instagramButton", { defaultValue: "Seguir @orapha.batista" })}
              </a>
            </Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-col items-center gap-2 space-y-0 text-center">
            <div className="rounded-lg bg-nav-icon/10 p-2">
              <HeartHandshake className="h-5 w-5 text-nav-icon" />
            </div>
            <div>
              <CardTitle className="text-lg">
                {t("pages.help.pixTitle", { defaultValue: "Apoie via PIX" })}
              </CardTitle>
              <CardDescription>
                {t("pages.help.pixDesc", { defaultValue: "Chave PIX (CNPJ) para contribuir com o desenvolvimento." })}
              </CardDescription>
            </div>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex flex-col items-center justify-center gap-3">
              <div className="rounded-lg border bg-white p-3 shadow-sm">
                <QRCodeSVG value={PIX_PAYLOAD} size={176} includeMargin={false} />
              </div>
            </div>
            <p className="text-center text-xs text-muted-foreground">
              {t("pages.help.pixNote", { defaultValue: "Escaneie o QR Code ou copie a chave acima." })}
            </p>
            <div className="mx-auto flex max-w-sm items-center justify-between gap-3 rounded-lg border bg-muted/30 px-4 py-3">
              <span className="font-mono text-sm">{PIX_KEY}</span>
              <Button
                variant="ghost"
                size="icon"
                onClick={copyPix}
                aria-label={t("pages.help.pixCopyAria", { defaultValue: "Copiar chave PIX" })}
              >
                <Copy className="h-4 w-4" />
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </AppShell>
  );
}
