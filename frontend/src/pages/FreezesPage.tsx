import { useEffect, useState } from 'react'
import { Button, Form, Input, Modal, Popconfirm, Select, Table, Tag } from 'antd'
import { Lock, Plus, RefreshCw, Unlock } from 'lucide-react'
import { PageHeader } from '../components/common/PageHeader'
import { useAuth } from '../hooks/useAuth'
import { useFreezeStore } from '../stores/freezeStore'
import { useTankStore } from '../stores/tankStore'
import type { FreezeInput, FreezeStatus, PeriodFreeze } from '../types/freeze'
import { dateTime, localInputDate } from '../utils/format'

const statusMeta: Record<FreezeStatus, { label: string; color: string }> = {
  pending_review: { label: '待复核', color: 'processing' },
  active: { label: '冻结中', color: 'error' },
  rejected: { label: '已驳回', color: 'warning' },
  released: { label: '已解除', color: 'default' }
}

export function FreezesPage() {
  const { can } = useAuth()
  const store = useFreezeStore()
  const tanks = useTankStore()
  const [open, setOpen] = useState(false)
  const [rejectTarget, setRejectTarget] = useState<PeriodFreeze | null>(null)
  const [releaseTarget, setReleaseTarget] = useState<PeriodFreeze | null>(null)
  const [form] = Form.useForm<FreezeInput>()
  const [rejectForm] = Form.useForm<{ review_note: string }>()
  const [releaseForm] = Form.useForm<{ release_reason: string }>()

  useEffect(() => { void Promise.all([store.load(), tanks.load()]) }, [])

  const openCreate = () => {
    form.setFieldsValue({
      tank_id: tanks.items[0]?.id,
      period_start: localInputDate(new Date(Date.now() - 24 * 3_600_000)),
      period_end: localInputDate(new Date())
    })
    setOpen(true)
  }
  const create = async (values: FreezeInput) => {
    await store.create({
      ...values,
      period_start: new Date(values.period_start).toISOString(),
      period_end: new Date(values.period_end).toISOString()
    })
    setOpen(false)
    form.resetFields()
  }
  const submitReject = async (values: { review_note: string }) => {
    if (rejectTarget) await store.reject(rejectTarget, values.review_note)
    setRejectTarget(null)
    rejectForm.resetFields()
  }
  const submitRelease = async (values: { release_reason: string }) => {
    if (releaseTarget) await store.release(releaseTarget, values.release_reason)
    setReleaseTarget(null)
    releaseForm.resetFields()
  }

  return (
    <>
      <PageHeader
        eyebrow="PERIOD FREEZE"
        title="期间冻结"
        description="平衡送审后锁定储罐期间；复核通过后期内补录快照、新增或确认/取消转移将被挡下并回报冻结记录编号，原数据与既有平衡结果保持可查。"
        actions={
          <>
            <Button icon={<RefreshCw size={16} />} onClick={() => void store.load()}>刷新</Button>
            {can('process_analyst', 'admin') && <Button type="primary" icon={<Plus size={16} />} onClick={openCreate}>登记冻结</Button>}
          </>
        }
      />
      <section className="data-panel">
        <div className="section-heading">
          <h2>储罐冻结记录</h2>
          <Select<FreezeStatus | ''>
            size="small" style={{ width: 140 }} value={store.statusFilter}
            onChange={(value) => void store.setStatusFilter(value)}
            options={[
              { value: '', label: '全部状态' },
              ...Object.entries(statusMeta).map(([value, meta]) => ({ value: value as FreezeStatus, label: meta.label }))
            ]}
          />
        </div>
        <Table<PeriodFreeze>
          rowKey="id"
          loading={store.loading}
          dataSource={store.items}
          pagination={{ pageSize: 12, showSizeChanger: false }}
          scroll={{ x: 1180 }}
          columns={[
            { title: '编号', dataIndex: 'id', width: 80, render: (id: number) => <span className="mono">#{id}</span> },
            { title: '储罐', key: 'tank', fixed: 'left', width: 120, render: (_, item) => item.tank?.tank_code ?? item.tank_id },
            { title: '期间开始', dataIndex: 'period_start', width: 150, render: dateTime },
            { title: '期间结束', dataIndex: 'period_end', width: 150, render: dateTime },
            { title: '冻结说明', dataIndex: 'freeze_note', ellipsis: true },
            {
              title: '状态', dataIndex: 'freeze_status', width: 100,
              render: (value: FreezeStatus) => <Tag icon={value === 'active' ? <Lock size={12} /> : undefined} color={statusMeta[value].color}>{statusMeta[value].label}</Tag>
            },
            { title: '登记人', width: 120, render: (_, item) => item.creator?.display_name ?? ('#' + item.created_by) },
            { title: '复核人/时间', width: 170, render: (_, item) => item.reviewed_at ? `${item.reviewer?.display_name ?? '#' + item.reviewed_by} · ${dateTime(item.reviewed_at)}` : '—' },
            { title: '解除人/时间', width: 170, render: (_, item) => item.released_at ? `${item.releaser?.display_name ?? '#' + item.released_by} · ${dateTime(item.released_at)}` : '—' },
            {
              title: '操作', key: 'action', fixed: 'right', width: 170,
              render: (_, item) => {
                if (item.freeze_status === 'pending_review' && can('reviewer', 'admin')) {
                  return (
                    <div className="table-actions">
                      <Popconfirm title="通过冻结？通过后该期间内现场变更将被锁定。" onConfirm={() => void store.approve(item)}>
                        <Button size="small" type="text">通过</Button>
                      </Popconfirm>
                      <Button size="small" type="text" danger onClick={() => setRejectTarget(item)}>驳回</Button>
                    </div>
                  )
                }
                if (item.freeze_status === 'active' && can('reviewer', 'admin')) {
                  return <Button size="small" type="text" icon={<Unlock size={15} />} onClick={() => setReleaseTarget(item)}>解除冻结</Button>
                }
                return null
              }
            }
          ]}
        />
      </section>

      <Modal title="登记储罐期间冻结" open={open} onCancel={() => setOpen(false)} footer={null} destroyOnClose>
        <Form<FreezeInput> form={form} layout="vertical" onFinish={create} requiredMark={false}>
          <div className="form-grid">
            <Form.Item className="span-2" name="tank_id" label="储罐" rules={[{ required: true }]}>
              <Select options={tanks.items.map((tank) => ({ value: tank.id, label: tank.tank_code + ' · ' + tank.name }))} />
            </Form.Item>
            <Form.Item name="period_start" label="期间开始" rules={[{ required: true }]}><Input type="datetime-local" /></Form.Item>
            <Form.Item name="period_end" label="期间结束" rules={[{ required: true }]}><Input type="datetime-local" /></Form.Item>
            <Form.Item className="span-2" name="freeze_note" label="冻结说明" rules={[{ required: true, min: 6, max: 500 }]}>
              <Input.TextArea rows={3} placeholder="说明本次冻结对应的平衡运行与复核范围" />
            </Form.Item>
          </div>
          <Button type="primary" htmlType="submit" block>提交冻结申请（待复核）</Button>
        </Form>
      </Modal>

      <Modal title="驳回冻结申请" open={rejectTarget !== null} onCancel={() => setRejectTarget(null)} footer={null} destroyOnClose>
        <Form layout="vertical" form={rejectForm} onFinish={submitReject} requiredMark={false}>
          <Form.Item name="review_note" label="复核说明" rules={[{ required: true, min: 6, max: 1000 }]}>
            <Input.TextArea rows={3} placeholder="说明驳回原因（不少于 6 个字符）" />
          </Form.Item>
          <Button type="primary" danger htmlType="submit" block>确认驳回</Button>
        </Form>
      </Modal>

      <Modal title="解除期间冻结" open={releaseTarget !== null} onCancel={() => setReleaseTarget(null)} footer={null} destroyOnClose>
        <Form layout="vertical" form={releaseForm} onFinish={submitRelease} requiredMark={false}>
          <p className="form-hint">解除后期内补录与转移操作将恢复，操作会记录操作者、时间和原因。</p>
          <Form.Item name="release_reason" label="解除原因" rules={[{ required: true, min: 6, max: 1000 }]}>
            <Input.TextArea rows={3} placeholder="说明解除冻结的业务原因（不少于 6 个字符）" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block>确认解除</Button>
        </Form>
      </Modal>
    </>
  )
}
