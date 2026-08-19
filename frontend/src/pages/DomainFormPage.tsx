import { useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Form, Input, Button, Switch, DatePicker, Select, Typography, Card, message, Space } from 'antd';
import { domainApi, tagApi, groupApi } from '../services';
import dayjs from 'dayjs';

const { Title } = Typography;
const { TextArea } = Input;

export function DomainFormPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [form] = Form.useForm();
  const isEdit = !!id;

  const { data: domainData } = useQuery({
    queryKey: ['domain', Number(id)],
    queryFn: () => domainApi.get(Number(id)),
    enabled: isEdit,
  });

  const { data: tagsData } = useQuery({ queryKey: ['tags'], queryFn: tagApi.list });
  const { data: groupsData } = useQuery({ queryKey: ['groups'], queryFn: groupApi.list });

  useEffect(() => {
    if (domainData?.data) {
      const d = domainData.data;
      form.setFieldsValue({
        domain_name: d.domain_name,
        registrar_identifier: d.registrar_identifier,
        expiration_date: d.expiration_date ? dayjs(d.expiration_date) : null,
        creation_date: d.creation_date ? dayjs(d.creation_date) : null,
        auto_renew: d.auto_renew,
        status: d.status,
        nameservers: d.nameservers?.join(', '),
        privacy_protection: d.privacy_protection,
        lock_status: d.lock_status,
        group_id: d.group_id,
        notes: d.notes,
        website_url: d.website_url,
        email_enabled: d.email_enabled,
        tag_ids: d.tags?.map(t => t.id),
      });
    }
  }, [domainData, form]);

  const createMutation = useMutation({
    mutationFn: (data: any) => domainApi.create(data),
    onSuccess: (result) => {
      message.success('Domain created');
      queryClient.invalidateQueries({ queryKey: ['domains'] });
      navigate(`/domains/${result.data.id}`);
    },
    onError: (e: Error) => message.error(e.message),
  });

  const updateMutation = useMutation({
    mutationFn: (data: any) => domainApi.update(Number(id), data),
    onSuccess: () => {
      message.success('Domain updated');
      queryClient.invalidateQueries({ queryKey: ['domains'] });
      queryClient.invalidateQueries({ queryKey: ['domain', Number(id)] });
      navigate(`/domains/${id}`);
    },
    onError: (e: Error) => message.error(e.message),
  });

  const handleSubmit = (values: any) => {
    const data: any = {
      domain_name: values.domain_name,
      registrar_identifier: values.registrar_identifier || '',
      expiration_date: values.expiration_date?.toISOString() || null,
      creation_date: values.creation_date?.toISOString() || null,
      auto_renew: values.auto_renew || false,
      status: values.status || 'active',
      nameservers: values.nameservers ? values.nameservers.split(',').map((s: string) => s.trim()).filter(Boolean) : [],
      privacy_protection: values.privacy_protection || false,
      lock_status: values.lock_status || false,
      group_id: values.group_id || null,
      notes: values.notes || '',
      website_url: values.website_url || '',
      email_enabled: values.email_enabled || false,
      tag_ids: values.tag_ids || [],
    };

    if (isEdit) {
      updateMutation.mutate(data);
    } else {
      createMutation.mutate(data);
    }
  };

  return (
    <div>
      <Title level={3}>{isEdit ? 'Edit Domain' : 'Add Domain'}</Title>
      <Card>
        <Form form={form} layout="vertical" onFinish={handleSubmit} style={{ maxWidth: 700 }}>
          <Form.Item name="domain_name" label="Domain Name" rules={[{ required: true, message: 'Domain name is required' }]}>
            <Input placeholder="example.com" disabled={isEdit} />
          </Form.Item>

          <Form.Item name="registrar_identifier" label="Registrar">
            <Input placeholder="GoDaddy, Cloudflare, etc." />
          </Form.Item>

          <Space size="large">
            <Form.Item name="expiration_date" label="Expiration Date">
              <DatePicker />
            </Form.Item>
            <Form.Item name="creation_date" label="Creation Date">
              <DatePicker />
            </Form.Item>
          </Space>

          <Form.Item name="status" label="Status">
            <Select options={[
              { value: 'active', label: 'Active' },
              { value: 'expired', label: 'Expired' },
              { value: 'unverified-removed', label: 'Unverified' },
            ]} />
          </Form.Item>

          <Form.Item name="nameservers" label="Nameservers (comma separated)">
            <Input placeholder="ns1.example.com, ns2.example.com" />
          </Form.Item>

          <Form.Item name="website_url" label="Website URL">
            <Input placeholder="https://example.com" />
          </Form.Item>

          <Space size="large">
            <Form.Item name="auto_renew" label="Auto Renew" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="privacy_protection" label="Privacy Protection" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="lock_status" label="Locked" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="email_enabled" label="Email Monitoring" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Space>

          <Form.Item name="tag_ids" label="Tags">
            <Select mode="multiple" placeholder="Select tags" options={tagsData?.data?.map(t => ({ value: t.id, label: t.name })) || []} />
          </Form.Item>

          <Form.Item name="group_id" label="Group">
            <Select allowClear placeholder="Select group" options={groupsData?.data?.map((g: any) => ({ value: g.id, label: g.name })) || []} />
          </Form.Item>

          <Form.Item name="notes" label="Notes">
            <TextArea rows={3} />
          </Form.Item>

          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit" loading={createMutation.isPending || updateMutation.isPending}>
                {isEdit ? 'Update' : 'Create'}
              </Button>
              <Button onClick={() => navigate(-1)}>Cancel</Button>
            </Space>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
}
