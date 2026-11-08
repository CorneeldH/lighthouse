import { CleanupList } from '@/components/CleanupList'
import { Header } from '@/components/Header'
import { QuestionList } from '@/components/QuestionList'
import { ReturnedList } from '@/components/ReturnedList'
import { NightBlock, ParkedBlock, QueueBlock, RunningBlock } from '@/components/RunBlocks'
import { UsagePanel } from '@/components/UsagePanel'

export default function App() {
  return (
    <main className="mx-auto max-w-3xl space-y-6 p-6">
      <Header />
      <UsagePanel />
      <RunningBlock />
      <ParkedBlock />
      <QueueBlock />
      <NightBlock />
      <QuestionList />
      <ReturnedList />
      <CleanupList />
    </main>
  )
}
