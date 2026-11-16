import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { useTranslations } from 'next-intl';

export default function NotFound() {
  const t = useTranslations('common.notFound');
  return (
    <div className="flex flex-col items-center justify-center h-screen bg-background">
      <Card className="w-full max-w-md p-8">
        <CardHeader>
          <CardTitle className="text-center text-hi text-2xl">
            {t('title')}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="text-center">
            <div className="text-muted-foreground">{t('description')}</div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
