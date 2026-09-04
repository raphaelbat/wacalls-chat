import { useTranslation } from "react-i18next";
import { AppShell } from "@/components/layout/AppShell";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  MessageSquare,
  KanbanSquare,
  PhoneCall,
  Users2,
  BarChart3,
  Settings,
  Code2,
  Zap,
  Palette,
  Workflow,
  Megaphone,
  Wifi,
  MonitorSmartphone,
} from "lucide-react";

const FeatureSection = ({ title, icon: Icon, items }: { title: string, icon: any, items: string[] }) => (
  <Card className="h-full">
    <CardHeader className="flex flex-row items-center gap-2 space-y-0">
      <div className="rounded-lg bg-nav-icon/10 p-2">
        <Icon className="h-5 w-5 text-nav-icon" />
      </div>
      <CardTitle className="text-lg">{title}</CardTitle>
    </CardHeader>
    <CardContent>
      <ul className="grid gap-2 text-sm text-muted-foreground">
        {items.map((item, i) => (
          <li key={i} className="flex items-start gap-2">
            <div className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-nav-icon/40" />
            <span>{item}</span>
          </li>
        ))}
      </ul>
    </CardContent>
  </Card>
);

const CATEGORY_ICONS: Record<string, any> = {
  chat: MessageSquare,
  productivity: Zap,
  flows: Workflow,
  campaigns: Megaphone,
  kanban: KanbanSquare,
  connections: Wifi,
  telephony: PhoneCall,
  queues: Users2,
  reports: BarChart3,
  admin: Settings,
  whitelabel: Palette,
  platform: MonitorSmartphone,
};

const CATEGORY_ORDER = [
  "chat",
  "productivity",
  "flows",
  "campaigns",
  "kanban",
  "connections",
  "telephony",
  "queues",
  "reports",
  "admin",
  "whitelabel",
  "platform",
];

export default function FeaturesPage() {
  const { t } = useTranslation();

  return (
    <AppShell>
      <div className="mx-auto max-w-6xl space-y-8 p-2">
        <div className="space-y-2">
          <h2 className="text-3xl font-bold tracking-tight">
            {t("pages.features.title", { defaultValue: "Funcionalidades do Sistema" })}
          </h2>
          <p className="text-muted-foreground">
            {t("pages.features.subtitle", {
              defaultValue: "Uma visão completa de todas as ferramentas e tecnologias integradas no WACalls Chat.",
            })}
          </p>
        </div>

        <div className="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
          {CATEGORY_ORDER.map((key) => {
            const items = t(`pages.features.categories.${key}.items`, { returnObjects: true, defaultValue: [] }) as string[];
            return (
              <FeatureSection
                key={key}
                title={t(`pages.features.categories.${key}.title`, { defaultValue: key })}
                icon={CATEGORY_ICONS[key]}
                items={Array.isArray(items) ? items : []}
              />
            );
          })}
        </div>

        <Card className="bg-muted/50">
          <CardHeader>
            <div className="flex items-center gap-2">
              <Code2 className="h-5 w-5 text-nav-icon" />
              <CardTitle>{t("pages.features.specsTitle", { defaultValue: "Especificações Técnicas" })}</CardTitle>
            </div>
          </CardHeader>
          <CardContent className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
            <div className="space-y-1">
              <span className="text-xs font-semibold uppercase text-muted-foreground">
                {t("pages.features.specs.frontend", { defaultValue: "Frontend" })}
              </span>
              <div className="flex flex-wrap gap-1">
                <Badge variant="outline">React 19</Badge>
                <Badge variant="outline">TypeScript</Badge>
                <Badge variant="outline">Tailwind CSS</Badge>
                <Badge variant="outline">Multi-idioma (i18next)</Badge>
              </div>
            </div>
            <div className="space-y-1">
              <span className="text-xs font-semibold uppercase text-muted-foreground">
                {t("pages.features.specs.backend", { defaultValue: "Backend" })}
              </span>
              <div className="flex flex-wrap gap-1">
                <Badge variant="outline">Go (Golang)</Badge>
                <Badge variant="outline">SQLite 3</Badge>
                <Badge variant="outline">SSE (Real-time)</Badge>
                <Badge variant="outline">REST API</Badge>
              </div>
            </div>
            <div className="space-y-1">
              <span className="text-xs font-semibold uppercase text-muted-foreground">
                {t("pages.features.specs.telephony", { defaultValue: "Telefonia" })}
              </span>
              <div className="flex flex-wrap gap-1">
                <Badge variant="outline">WebRTC</Badge>
                <Badge variant="outline">SRTP / RTP</Badge>
                <Badge variant="outline">STUN</Badge>
                <Badge variant="outline">Codec próprio (MLow)</Badge>
              </div>
            </div>
            <div className="space-y-1">
              <span className="text-xs font-semibold uppercase text-muted-foreground">
                {t("pages.features.specs.integrations", { defaultValue: "Integrações" })}
              </span>
              <div className="flex flex-wrap gap-1">
                <Badge variant="outline">n8n (webhook)</Badge>
                <Badge variant="outline">Requisição HTTP</Badge>
                <Badge variant="outline">SMTP</Badge>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    </AppShell>
  );
}
