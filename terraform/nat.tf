module "fck_nat" {
  source  = "RaJiska/fck-nat/aws"
  version = "1.4.0"

  name          = "${var.project_name}-nat"
  vpc_id        = aws_vpc.main.id
  subnet_id     = aws_subnet.public.id
  instance_type = "t4g.nano"

  update_route_tables = true
  route_tables_ids = {
    "private" = aws_route_table.private.id
  }

  tags = {
    Name = "${var.project_name}-nat"
  }
}
